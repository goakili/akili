<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, ref } from 'vue'
import { api, type Approval } from '../api'
import { useAuth } from '../stores/auth'
import { useCatalog } from '../stores/catalog'
import { useToast } from '../stores/toast'
import { countdown, fmtDate, relTime } from '../lib/format'
import { useNow } from '../lib/now'
import Badge from './Badge.vue'
import JsonBlock from './JsonBlock'
import PlanView from './PlanView.vue'
import Icon from './Icon'
import { parsePlan } from '../lib/tools'

const props = withDefaults(
  defineProps<{ approval: Approval; showContext?: boolean; showInput?: boolean; keyboard?: boolean; flat?: boolean; /** The change a change_run approval proposes. */ changeId?: string }>(),
  {
    showContext: false,
    showInput: true,
    keyboard: false,
    flat: false,
    changeId: '',
  },
)
const emit = defineEmits<{ (e: 'resolved', a: Approval): void }>()

const auth = useAuth()
const catalog = useCatalog()
const toast = useToast()
const now = useNow()
const note = ref('')
const busy = ref(false)
const noteId = `note-${Math.random().toString(36).slice(2, 8)}`

const pending = computed(() => props.approval.status === 'pending')
// A change_run approval approves a whole plan: show it readably, never as raw JSON.
const plan = computed(() => (props.approval.tool === 'change_run' ? parsePlan(props.approval.input) : null))
const what = computed(() => (plan.value ? 'change plan' : props.approval.tool))
const left = computed(() => countdown(props.approval.expires_at, now.value))
const expired = computed(() => pending.value && left.value === 'expired')
const urgent = computed(() => pending.value && new Date(props.approval.expires_at).getTime() - now.value < 60_000)
const canDecide = computed(() => pending.value && auth.isOperator && !expired.value && !busy.value)

async function decide(allow: boolean) {
  if (!canDecide.value) return
  busy.value = true
  try {
    const a = allow ? await api.approve(props.approval.id, note.value.trim()) : await api.deny(props.approval.id, note.value.trim())
    toast.success(`${allow ? 'Approved' : 'Denied'} ${plan.value ? `plan “${plan.value.title}”` : a.tool} on ${catalog.agentName(a.agent_id)}`)
    emit('resolved', a)
  } catch {
    /* toasted (409 when someone else decided first) */
  } finally {
    busy.value = false
  }
}

// Keyboard: A approves, D denies, when the card itself has focus (not while typing a note).
function onKey(e: KeyboardEvent) {
  if (!props.keyboard || e.target !== e.currentTarget || e.metaKey || e.ctrlKey || e.altKey) return
  const k = e.key.toLowerCase()
  if (k === 'a') {
    e.preventDefault()
    decide(true)
  } else if (k === 'd') {
    e.preventDefault()
    decide(false)
  }
}
</script>

<template>
  <div
    class="approval-card"
    :class="[{ resolved: !pending, flat, 'is-plan': !!plan }, flat ? approval.risk : '']"
    role="group"
    :aria-label="`Approval for ${what}${keyboard && pending ? '. Press A to approve or D to deny.' : ''}`"
    :tabindex="keyboard ? 0 : undefined"
    @keydown="onKey"
  >
    <div class="row wrap between">
      <div class="ac-title">
        <Icon name="approvals" class="shield" />
        <strong>{{ pending ? 'Approval needed' : 'Approval' }}</strong>
        <span v-if="plan" class="strong">change plan</span>
        <span v-else class="mono strong">{{ approval.tool }}</span>
        <Badge :value="approval.risk" kind="risk" :label="plan ? `highest risk: ${approval.risk}` : undefined" />
        <Badge v-if="!pending" :value="approval.status" />
      </div>
      <span v-if="pending" class="countdown" :class="{ urgent: urgent || expired }" :title="`Expires ${fmtDate(approval.expires_at)}`">
        <Icon name="clock" />{{ expired ? 'expired' : `expires in ${left}` }}
      </span>
    </div>
    <div v-if="showContext" class="ac-ctx">
      <span><Icon name="agents" /><RouterLink :to="`/agents/${approval.agent_id}`">{{ catalog.agentName(approval.agent_id) }}</RouterLink></span>
      <span><Icon name="chat" /><RouterLink :to="`/sessions/${approval.session_id}`">Session</RouterLink></span>
      <span v-if="approval.task_id"><Icon name="tasks" /><RouterLink :to="`/tasks/${approval.task_id}`">Task</RouterLink></span>
      <span v-if="changeId"><Icon name="clipboard" /><RouterLink :to="`/changes/${changeId}`">Change</RouterLink></span>
      <span :title="fmtDate(approval.created_at)"><Icon name="clock" />requested {{ relTime(approval.created_at, now) }}</span>
    </div>
    <div v-if="approval.reason && !plan" class="small" style="color: var(--text-secondary)">{{ approval.reason }}</div>
    <template v-if="showInput">
      <PlanView v-if="plan" :plan="plan" :hide-title="false" />
      <JsonBlock v-else :value="approval.input" max-height="260px" />
    </template>
    <div v-if="plan && pending" class="small muted">
      Approving runs every step, then every check; if anything fails the agent runs the rollback. Each call must match this plan exactly.
    </div>
    <div v-if="!pending" class="small muted">
      {{ approval.status }}{{ approval.decided_at ? ' ' + relTime(approval.decided_at, now) : '' }}{{ approval.note ? ` · “${approval.note}”` : '' }}
    </div>
    <form v-else-if="auth.isOperator" class="row wrap" @submit.prevent="decide(true)">
      <label :for="noteId" class="sr-only">Note</label>
      <input :id="noteId" v-model="note" class="input sm grow" maxlength="500" placeholder="Optional note for the audit log" style="min-width: 160px" />
      <button type="button" class="btn btn-sm" :disabled="!canDecide" @click="decide(false)"><Icon name="x" />Deny<kbd v-if="keyboard" class="hide-mobile">D</kbd></button>
      <button type="submit" class="btn btn-sm btn-primary" :disabled="!canDecide">
        <span v-if="busy" class="spinner" /><Icon v-else name="check" />{{ plan ? 'Approve plan' : 'Approve' }}<kbd v-if="keyboard" class="hide-mobile" style="background: transparent; color: inherit; border-color: rgb(255 255 255 / 50%)">A</kbd>
      </button>
    </form>
    <div v-else class="small muted">Operators can approve or deny this {{ plan ? 'plan' : 'call' }}.</div>
  </div>
</template>
