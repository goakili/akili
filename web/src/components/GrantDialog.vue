<!--
  Copyright (c) 2026 Jonas Kaninda.
  SPDX-License-Identifier: MIT
-->
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api, type Approval, type ApprovalGrant, type Risk } from '../api'
import { useCatalog } from '../stores/catalog'
import { useToast } from '../stores/toast'
import { argRows, GRANT_DURATIONS, toMatch, type ArgRow } from '../lib/grants'
import { splitList } from '../lib/format'
import Modal from './Modal.vue'
import Icon from './Icon'

const props = defineProps<{ open: boolean; approval: Approval | null }>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'granted', g: ApprovalGrant, a: Approval | null): void }>()

const catalog = useCatalog()
const toast = useToast()

const rows = ref<ArgRow[]>([])
const scope = ref<'agent' | 'labels'>('agent')
const labels = ref('')
const hours = ref(2)
const maxUses = ref(0)
const windowed = ref(false)
const reason = ref('')
const saving = ref(false)

watch(
  () => [props.open, props.approval?.id],
  () => {
    if (!props.open || !props.approval) return
    rows.value = argRows(props.approval.input)
    scope.value = 'agent'
    labels.value = ''
    hours.value = 2
    maxUses.value = 0
    windowed.value = false
    reason.value = ''
  },
  { immediate: true },
)

const match = computed(() => (props.approval ? toMatch(rows.value, props.approval.input) : null))
const valid = computed(() => !!match.value && reason.value.trim() !== '' && (scope.value === 'agent' || splitList(labels.value).length > 0))

async function submit() {
  const a = props.approval
  if (!a || !valid.value) return
  saving.value = true
  try {
    let decided: Approval | null = null
    if (a.status === 'pending') decided = await api.approve(a.id, `approved and granted: ${reason.value.trim()}`)
    const g = await api.createGrant({
      agent_id: scope.value === 'agent' ? a.agent_id : null,
      labels: scope.value === 'labels' ? splitList(labels.value) : [],
      tool: a.tool,
      match: match.value ?? {},
      max_risk: a.risk as Risk,
      windowed: windowed.value,
      expires_in_hours: hours.value,
      max_uses: maxUses.value,
      reason: reason.value.trim(),
      approval_id: a.id,
    })
    toast.success(`Granted ${a.tool} for ${GRANT_DURATIONS.find(([h]) => h === hours.value)?.[1] ?? `${hours.value} hours`}`)
    emit('granted', g, decided)
  } catch {
    /* toasted */
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <Modal :open="open" :title="approval?.status === 'pending' ? 'Approve and allow similar calls' : 'Allow similar calls'" wide :dismissable="!saving" @close="emit('close')">
    <form v-if="approval" id="grant-form" class="stack" @submit.prevent="submit">
      <p class="small muted" style="margin: 0">
        Calls of <span class="mono strong">{{ approval.tool }}</span> ({{ approval.risk }} risk) that match every argument below run without asking until the grant expires.
        Policy denials and critical calls are never covered. Every use is in the audit log, and the kill switch revokes all grants.
      </p>
      <div class="choices" role="radiogroup" aria-label="Agents">
        <label class="choice" :class="{ on: scope === 'agent' }">
          <input v-model="scope" type="radio" name="gr-scope" value="agent" /><Icon name="agents" />
          <span><span class="c-title">This agent</span><br /><span class="c-sub">{{ catalog.agentName(approval.agent_id) }}</span></span>
        </label>
        <label class="choice" :class="{ on: scope === 'labels' }">
          <input v-model="scope" type="radio" name="gr-scope" value="labels" /><Icon name="layers" />
          <span><span class="c-title">By labels</span><br /><span class="c-sub">Any agent with every label.</span></span>
        </label>
      </div>
      <div v-if="scope === 'labels'" class="field">
        <label for="gr-labels">Labels</label>
        <input id="gr-labels" v-model="labels" class="input" placeholder="prod, eu" />
      </div>
      <div class="field">
        <span class="label">Arguments</span>
        <div v-if="!rows.length" class="small muted">This call takes no arguments.</div>
        <div v-for="r in rows" :key="r.key" class="row wrap" style="gap: 8px; margin-bottom: 6px">
          <code class="label-chip" style="min-width: 110px">{{ r.key }}</code>
          <select v-model="r.mode" class="select sm" :aria-label="`How ${r.key} may vary`" style="width: 130px">
            <option value="equals">exactly</option>
            <option value="glob">matches glob</option>
            <option value="one_of">one of</option>
            <option value="range">in range</option>
            <option value="any">any value</option>
          </select>
          <template v-if="r.mode === 'range'">
            <input v-model="r.min" class="input sm" type="number" placeholder="min" :aria-label="`${r.key} minimum`" style="width: 90px" />
            <input v-model="r.max" class="input sm" type="number" placeholder="max" :aria-label="`${r.key} maximum`" style="width: 90px" />
          </template>
          <input v-else-if="r.mode !== 'any'" v-model="r.value" class="input sm mono grow" :aria-label="`${r.key} value`"
            :placeholder="r.mode === 'one_of' ? 'a, b, c' : r.mode === 'glob' ? 'api-*' : ''" />
        </div>
      </div>
      <div class="grid-2">
        <div class="field">
          <label for="gr-hours">Expires after</label>
          <select id="gr-hours" v-model.number="hours" class="select">
            <option v-for="[h, label] in GRANT_DURATIONS" :key="h" :value="h">{{ label }}</option>
          </select>
        </div>
        <div class="field">
          <label for="gr-uses">Maximum uses</label>
          <input id="gr-uses" v-model.number="maxUses" class="input" type="number" min="0" />
          <span class="hint">0 means no limit until it expires.</span>
        </div>
      </div>
      <label class="switch"><input v-model="windowed" type="checkbox" />Only during change windows</label>
      <div class="field">
        <label for="gr-reason">Why is this safe? <span class="req">*</span></label>
        <input id="gr-reason" v-model="reason" class="input" maxlength="500" required placeholder="Routine restarts of the staging API during the migration" />
      </div>
    </form>
    <template #footer>
      <button type="button" class="btn" @click="emit('close')">Cancel</button>
      <button type="submit" form="grant-form" class="btn btn-primary" :disabled="saving || !valid">
        <span v-if="saving" class="spinner" />{{ approval?.status === 'pending' ? 'Approve and grant' : 'Grant' }}
      </button>
    </template>
  </Modal>
</template>
