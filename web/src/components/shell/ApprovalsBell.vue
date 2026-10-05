<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api, type Approval, type Question } from '../../api'
import { useAuth } from '../../stores/auth'
import { useCatalog } from '../../stores/catalog'
import { useLive } from '../../stores/live'
import { useToast } from '../../stores/toast'
import { countdown } from '../../lib/format'
import { useNow } from '../../lib/now'
import { usePopover } from '../../lib/popover'
import Icon from '../Icon'
import Badge from '../Badge.vue'
import PlanView from '../PlanView.vue'
import { callSummary, parsePlan } from '../../lib/tools'

const auth = useAuth()
const catalog = useCatalog()
const live = useLive()
const toast = useToast()
const now = useNow()

const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLElement | null>(null)
const { open, toggle, close } = usePopover(root, trigger)
const items = ref<Approval[]>([])
const questions = ref<Question[]>([])
const total = computed(() => live.pendingApprovals + live.pendingQuestions)
const loading = ref(false)
const busy = ref<string | null>(null)

async function load() {
  loading.value = true
  try {
    const [a, q] = await Promise.all([
      api.listApprovals({ status: 'pending', size: 6 }, { quiet: true }),
      api.pageQuestions({ status: 'pending', size: 6 }, { quiet: true }).catch(() => null),
    ])
    items.value = a ?? []
    questions.value = q?.items ?? []
    catalog.loadAgents()
  } catch {
    /* the badge count still shows */
  } finally {
    loading.value = false
  }
}

watch(open, (o) => o && load())
watch(
  () => [live.pendingApprovals, live.pendingQuestions],
  () => open.value && load(),
)

async function answer(q: Question, choice: number) {
  busy.value = q.id
  try {
    await api.answerQuestion(q.id, { choice })
    toast.success(`Answer sent to ${catalog.agentName(q.agent_id)}`)
    questions.value = questions.value.filter((x) => x.id !== q.id)
    live.refreshCounts()
  } catch {
    /* toasted */
  } finally {
    busy.value = null
  }
}

const questionLink = (q: Question) => (q.task_id ? `/tasks/${q.task_id}` : `/sessions/${q.session_id}`)

async function decide(a: Approval, allow: boolean) {
  busy.value = a.id
  try {
    if (allow) await api.approve(a.id)
    else await api.deny(a.id)
    const plan = planOf(a)
    toast.success(`${allow ? 'Approved' : 'Denied'} ${plan ? `plan “${plan.title}”` : a.tool} on ${catalog.agentName(a.agent_id)}`)
    items.value = items.value.filter((x) => x.id !== a.id)
    live.refreshCounts()
  } catch {
    /* toasted */
  } finally {
    busy.value = null
  }
}

function summary(a: Approval): string {
  return callSummary(a.input, a.tool) || a.reason
}

function planOf(a: Approval) {
  return a.tool === 'change_run' ? parsePlan(a.input) : null
}
</script>

<template>
  <div ref="root" class="popover-anchor">
    <button
      ref="trigger"
      type="button"
      class="icon-btn"
      :aria-label="total ? `Waiting on you: ${live.pendingApprovals} approvals, ${live.pendingQuestions} questions` : 'Nothing waiting on you'"
      aria-haspopup="true"
      :aria-expanded="open"
      @click="toggle"
    >
      <Icon name="bell" />
      <span v-if="total > 0" class="bell-count" aria-hidden="true">{{ total > 99 ? '99+' : total }}</span>
    </button>
    <div v-if="open" class="menu wide" role="dialog" aria-label="Waiting on you">
      <div class="menu-head row between">
        <strong>Waiting on you</strong>
        <span v-if="total" class="badge accent">{{ total }}</span>
      </div>
      <div v-if="loading && !items.length && !questions.length" class="stack tight" style="padding: 10px">
        <span class="skel" style="width: 70%" /><span class="skel" style="width: 50%" />
      </div>
      <div v-else-if="!items.length && !questions.length" class="empty compact">
        <strong>You're all caught up</strong>
        <p class="small">No approvals or questions are waiting for you.</p>
      </div>
      <div v-else style="max-height: 420px; overflow-y: auto">
        <div v-for="q in questions" :key="q.id" class="bell-item">
          <div class="row between">
            <span class="row strong" style="min-width: 0"><Icon name="help" :size="14" />Question</span>
            <span class="xs muted nowrap num">{{ countdown(q.expires_at, now) }}</span>
          </div>
          <div class="small bell-question">{{ q.question }}</div>
          <div class="small muted truncate">
            <RouterLink :to="`/agents/${q.agent_id}`" @click="close">{{ catalog.agentName(q.agent_id) }}</RouterLink>
          </div>
          <div class="row wrap">
            <template v-if="auth.isOperator">
              <button
                v-for="(o, i) in q.options ?? []"
                :key="i"
                type="button"
                class="btn btn-xs"
                :class="{ 'btn-primary': o.recommended }"
                :title="o.description || (o.recommended ? 'The agent recommends this option' : undefined)"
                :disabled="busy === q.id"
                @click="answer(q, i)"
              >
                {{ o.label }}
              </button>
            </template>
            <RouterLink :to="questionLink(q)" class="small" style="margin-left: auto" @click="close">{{ auth.isOperator ? 'Something else…' : 'Open' }}</RouterLink>
          </div>
        </div>
        <div v-for="a in items" :key="a.id" class="bell-item">
          <div class="row between">
            <div class="row" style="min-width: 0">
              <span v-if="planOf(a)" class="strong truncate"><Icon name="clipboard" :size="13" style="vertical-align: -2px" /> Change plan</span>
              <span v-else class="mono strong truncate">{{ a.tool }}</span>
              <Badge :value="a.risk" kind="risk" />
            </div>
            <span class="xs muted nowrap num">{{ countdown(a.expires_at, now) }}</span>
          </div>
          <PlanView v-if="planOf(a)" :plan="planOf(a)!" compact />
          <div class="small muted truncate">
            <RouterLink :to="`/agents/${a.agent_id}`" @click="close">{{ catalog.agentName(a.agent_id) }}</RouterLink>
            <template v-if="!planOf(a)"> · <span class="mono">{{ summary(a) }}</span></template>
          </div>
          <div class="row">
            <template v-if="auth.isOperator">
              <button type="button" class="btn btn-primary btn-xs" :disabled="busy === a.id" @click="decide(a, true)"><Icon name="check" />{{ planOf(a) ? 'Approve plan' : 'Approve' }}</button>
              <button type="button" class="btn btn-xs" :disabled="busy === a.id" @click="decide(a, false)">Deny</button>
            </template>
            <RouterLink v-if="planOf(a)" to="/approvals" class="small" style="margin-left: auto" @click="close">Review full plan</RouterLink>
            <RouterLink v-else :to="`/sessions/${a.session_id}`" class="small" style="margin-left: auto" @click="close">Open session</RouterLink>
          </div>
        </div>
      </div>
      <div class="menu-sep" />
      <RouterLink to="/approvals" class="menu-item" style="justify-content: center; font-weight: 600" @click="close">
        View all approvals<Icon name="arrowRight" />
      </RouterLink>
    </div>
  </div>
</template>
