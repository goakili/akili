<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, ApiError, type Approval, type Change, type Overview, type Task } from '../api'
import { useAuth } from '../stores/auth'
import { useLive } from '../stores/live'
import { useCatalog } from '../stores/catalog'
import { useCoder } from '../stores/coder'
import { useToast } from '../stores/toast'
import { usd, relTime, countdown, duration } from '../lib/format'
import { fetchUntil } from '../lib/paged'
import { useNow } from '../lib/now'
import Badge from '../components/Badge.vue'
import Icon, { type IconName } from '../components/Icon'
import EmptyState from '../components/EmptyState.vue'
import PrLink from '../components/PrLink.vue'
import PlanView from '../components/PlanView.vue'
import { callSummary, parsePlan } from '../lib/tools'

const auth = useAuth()
const live = useLive()
const catalog = useCatalog()
const coder = useCoder()
const toast = useToast()
const router = useRouter()
const now = useNow()

const ov = ref<Overview | null>(null)
const tasks = ref<Task[]>([])
const approvals = ref<Approval[]>([])
/** Pending change plans, by approval id (plans are shown first, with their summary). */
const pendingChanges = ref<Map<string, Change>>(new Map())
/** null = not visible to this role */
const providerCount = ref<number | null>(null)
const loading = ref(true)
const deciding = ref<string | null>(null)
const opening = ref<string | null>(null)

async function load() {
  try {
    const [o, t, a, ch] = await Promise.all([
      api.overview(),
      api.listTasks({ size: 8 }),
      api.listApprovals({ status: 'pending', size: 20 }),
      api.listChanges({ status: 'pending', size: 20 }, { quiet: true }).catch(() => null),
      catalog.loadAgents(true),
    ])
    ov.value = o
    tasks.value = t ?? []
    pendingChanges.value = new Map((ch ?? []).map((c) => [c.approval_id, c]))
    // Change plans first: one decision covers a whole fix.
    approvals.value = [...(a ?? [])].sort((x, y) => Number(y.tool === 'change_run') - Number(x.tool === 'change_run')).slice(0, 5)
  } catch {
    /* toasted */
  } finally {
    loading.value = false
  }
  loadCoding()
  if (auth.isAdmin) {
    try {
      providerCount.value = ((await api.listProviders({ quiet: true })) ?? []).length
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) providerCount.value = null
    }
  }
}

// ---- coding: pull requests opened by tasks in the last 7 days --------------------------------------
const prTasks = ref<Task[]>([])
async function loadCoding() {
  await coder.loadProjects()
  if (!coder.projects.length) return
  try {
    const since = Date.now() - 7 * 86400_000
    prTasks.value = await fetchUntil((page) => api.pageTasks({ has_pr: true, page, size: 200 }, { quiet: true }), (t) => new Date(t.created_at).getTime() < since)
  } catch {
    /* optional card */
  }
}

let timer: ReturnType<typeof setTimeout> | null = null
function scheduleReload() {
  if (timer) clearTimeout(timer)
  timer = setTimeout(load, 700)
}

const RELEVANT = new Set(['task.updated', 'change.updated', 'approval.created', 'approval.resolved', 'agent.status', 'system.kill_switch', 'usage', 'session.created', 'session.closed'])
let offEvent: (() => void) | null = null
let offReconnect: (() => void) | null = null
onMounted(() => {
  load()
  offEvent = live.on((ev) => {
    if (ev.type === 'task.updated' && ev.data) {
      const t = ev.data as Task
      const i = tasks.value.findIndex((x) => x.id === t.id)
      if (i >= 0) tasks.value[i] = t
    }
    if (ev.type === 'agent.status' && ev.agent_id) catalog.patchAgentStatus(ev.agent_id, (ev.data as { status?: string } | undefined)?.status ?? 'offline')
    if (RELEVANT.has(ev.type)) scheduleReload()
  })
  offReconnect = live.onReconnect(load)
})
onUnmounted(() => {
  offEvent?.()
  offReconnect?.()
  if (timer) clearTimeout(timer)
})

// ---- greeting & quick actions ---------------------------------------------------------------------

const greeting = computed(() => {
  const h = new Date(now.value).getHours()
  const part = h < 5 ? 'Good evening' : h < 12 ? 'Good morning' : h < 18 ? 'Good afternoon' : 'Good evening'
  const name = auth.user?.name?.split(' ')[0] || auth.user?.email?.split('@')[0] || ''
  return name ? `${part}, ${name}` : part
})

const quick = computed(() => {
  const out: { label: string; icon: IconName; to: string }[] = []
  if (auth.isAdmin) out.push({ label: 'Add agent', icon: 'agents', to: '/agents?new=1' })
  if (auth.isOperator) out.push({ label: 'New task', icon: 'tasks', to: '/tasks?new=1' })
  if (auth.isOperator) out.push({ label: 'New chat', icon: 'chat', to: '/sessions?new=1' })
  out.push({ label: 'Review approvals', icon: 'approvals', to: '/approvals' })
  if (auth.isAdmin) out.push({ label: 'Add policy', icon: 'policies', to: '/policies?new=1' })
  return out
})

// ---- setup checklist ---------------------------------------------------------------------------

const steps = computed(() => {
  const agents = ov.value?.agents.total ?? catalog.agents.length
  const anyTask = tasks.value.length > 0
  const providerDone = providerCount.value === null ? agents > 0 || anyTask : providerCount.value > 0
  return [
    { title: 'Add a model provider', text: 'Connect Anthropic, an OpenAI-compatible endpoint or a local model.', done: providerDone, to: '/settings?tab=providers', cta: 'Add provider', allowed: auth.isAdmin },
    { title: 'Add an agent', text: 'Enroll a host with a one-time join token and bind a policy.', done: agents > 0, to: '/agents?new=1', cta: 'Add agent', allowed: auth.isAdmin },
    { title: 'Run a task', text: 'Give an agent a goal; risky steps come to you for approval.', done: anyTask, to: '/tasks?new=1', cta: 'New task', allowed: auth.isOperator },
  ]
})
const setupDone = computed(() => steps.value.every((s) => s.done))

// ---- fleet ---------------------------------------------------------------------------------------

const RANK: Record<string, number> = { online: 0, pending: 1, offline: 2, revoked: 3 }
const fleet = computed(() =>
  [...catalog.agents].sort((a, b) => (RANK[a.status] ?? 9) - (RANK[b.status] ?? 9) || a.name.localeCompare(b.name)).slice(0, 8),
)

async function openChat(agentId: string) {
  opening.value = agentId
  try {
    const s = await api.createSession(agentId)
    router.push(`/sessions/${s.id}`)
  } catch {
    /* toasted */
  } finally {
    opening.value = null
  }
}

// ---- approvals -----------------------------------------------------------------------------------

async function decide(a: Approval, allow: boolean) {
  deciding.value = a.id
  try {
    if (allow) await api.approve(a.id)
    else await api.deny(a.id)
    const plan = planOf(a)
    toast.success(`${allow ? 'Approved' : 'Denied'} ${plan ? `plan “${plan.title}”` : a.tool} on ${catalog.agentName(a.agent_id)}`)
    approvals.value = approvals.value.filter((x) => x.id !== a.id)
    live.refreshCounts()
  } catch {
    /* toasted */
  } finally {
    deciding.value = null
  }
}

function approvalSummary(a: Approval): string {
  return callSummary(a.input, a.tool) || a.reason
}

function planOf(a: Approval) {
  return a.tool === 'change_run' ? parsePlan(a.input) : null
}

function taskDuration(t: Task): string {
  if (!t.started_at) return '—'
  const start = new Date(t.started_at).getTime()
  const end = t.finished_at ? new Date(t.finished_at).getTime() : now.value
  if (!start || end < start) return '—'
  return duration(end - start)
}
</script>

<template>
  <div class="stack loose">
    <header class="page-head" style="margin-bottom: 0">
      <div class="ph-text">
        <h1>{{ greeting }}</h1>
        <p class="sub" style="margin-bottom: 0">Here is your fleet at a glance: health, work in flight and decisions waiting on you.</p>
      </div>
    </header>

    <nav class="qa-row" aria-label="Quick actions">
      <RouterLink v-for="q in quick" :key="q.label" :to="q.to" class="qa-chip">
        <span class="qa-ic" aria-hidden="true"><Icon :name="q.icon" /></span>{{ q.label }}
      </RouterLink>
    </nav>

    <!-- stat cards -->
    <div v-if="loading && !ov" class="stats" aria-busy="true">
      <div v-for="i in 5" :key="i" class="skel skel-card" />
    </div>
    <div v-else-if="ov" class="stats">
      <RouterLink to="/agents" class="stat">
        <span class="stat-icon tone-success" aria-hidden="true"><Icon name="agents" /></span>
        <div>
          <div class="stat-label">Agents online</div>
          <div class="stat-value">{{ ov.agents.online }}<small> / {{ ov.agents.total }}</small></div>
          <div class="stat-sub">{{ ov.agents.pending ? `${ov.agents.pending} not enrolled yet` : 'all enrolled' }}</div>
        </div>
      </RouterLink>
      <RouterLink to="/tasks" class="stat">
        <span class="stat-icon" aria-hidden="true"><Icon name="loader" /></span>
        <div>
          <div class="stat-label">Running tasks</div>
          <div class="stat-value">{{ ov.tasks.running }}</div>
          <div class="stat-sub">{{ ov.tasks.queued }} queued</div>
        </div>
      </RouterLink>
      <RouterLink to="/approvals" class="stat">
        <span class="stat-icon tone-warning" aria-hidden="true"><Icon name="approvals" /></span>
        <div>
          <div class="stat-label">Pending approvals</div>
          <div class="stat-value">{{ ov.pending_approvals }}</div>
          <div class="stat-sub">{{ ov.pending_approvals ? 'waiting on a human' : 'nothing waiting' }}</div>
        </div>
      </RouterLink>
      <RouterLink to="/settings?tab=usage" class="stat">
        <span class="stat-icon tone-info" aria-hidden="true"><Icon name="dollar" /></span>
        <div>
          <div class="stat-label">Spend today</div>
          <div class="stat-value">{{ usd(ov.spend_today_usd) }}</div>
          <div class="stat-sub">{{ usd(ov.spend_month_usd) }} this month</div>
        </div>
      </RouterLink>
      <RouterLink to="/tasks?view=list" class="stat">
        <span class="stat-icon" :class="ov.tasks.failed_24h ? 'tone-danger' : 'tone-neutral'" aria-hidden="true"><Icon name="xCircle" /></span>
        <div>
          <div class="stat-label">Failed · 24h</div>
          <div class="stat-value">{{ ov.tasks.failed_24h }}</div>
          <div class="stat-sub">{{ ov.tasks.succeeded_24h }} succeeded</div>
        </div>
      </RouterLink>
    </div>

    <!-- first-time setup -->
    <section v-if="ov && !setupDone" class="card" aria-labelledby="setup-title">
      <div class="card-head">
        <h2 id="setup-title"><Icon name="sparkles" />Get started</h2>
        <span class="small muted">{{ steps.filter((s) => s.done).length }} of 3 done</span>
      </div>
      <div class="card-body">
        <ol class="setup" style="list-style: none; margin: 0; padding: 0">
          <li v-for="(s, i) in steps" :key="s.title" class="setup-step" :class="{ done: s.done }">
            <span class="num" aria-hidden="true"><Icon v-if="s.done" name="check" :size="14" /><template v-else>{{ i + 1 }}</template></span>
            <div class="grow">
              <div class="s-title">{{ s.title }}<span class="sr-only">{{ s.done ? ' (done)' : ' (to do)' }}</span></div>
              <p>{{ s.text }}</p>
              <RouterLink v-if="!s.done && s.allowed" :to="s.to" class="btn btn-sm" :class="{ 'btn-primary': steps.findIndex((x) => !x.done) === i }">
                {{ s.cta }}<Icon name="arrowRight" />
              </RouterLink>
              <span v-else-if="!s.done" class="small muted">Ask an admin to do this.</span>
            </div>
          </li>
        </ol>
      </div>
    </section>

    <div class="dash-grid">
      <div class="stack loose">
        <!-- fleet -->
        <section class="card" aria-labelledby="fleet-title">
          <div class="card-head">
            <h2 id="fleet-title"><Icon name="server" />Fleet</h2>
            <RouterLink to="/agents">All agents</RouterLink>
          </div>
          <div v-if="loading && !catalog.agents.length" class="list">
            <div v-for="i in 3" :key="i" class="list-row"><span class="skel" style="width: 40%" /></div>
          </div>
          <EmptyState v-else-if="!fleet.length" title="No agents yet" icon="agents" compact>
            Enroll a host to start delegating work.
            <template v-if="auth.isAdmin" #actions><RouterLink to="/agents?new=1" class="btn btn-primary btn-sm"><Icon name="plus" />Add agent</RouterLink></template>
          </EmptyState>
          <div v-else class="list">
            <div v-for="a in fleet" :key="a.id" class="list-row">
              <span class="status-dot" :class="a.status" aria-hidden="true" />
              <div class="grow">
                <RouterLink :to="`/agents/${a.id}`" class="strong" style="color: var(--text-primary)">{{ a.name }}</RouterLink>
                <div class="small muted truncate">
                  <span class="sr-only">{{ a.status }} · </span>{{ a.facts?.hostname || 'not enrolled' }}<template v-if="a.facts?.os"> · {{ a.facts.os }}/{{ a.facts.arch }}</template>
                </div>
              </div>
              <span class="small muted nowrap hide-mobile" :title="`${a.active_sessions} active sessions`"><Icon name="chat" :size="13" style="vertical-align: -2px" /> {{ a.active_sessions }}</span>
              <Badge :value="a.draining ? 'draining' : a.status" />
              <button
                v-if="auth.isOperator && a.status === 'online'"
                type="button"
                class="btn btn-xs"
                :disabled="opening === a.id"
                :aria-label="`Open chat with ${a.name}`"
                @click="openChat(a.id)"
              >
                <Icon name="chat" />Chat
              </button>
            </div>
          </div>
        </section>

        <!-- recent tasks -->
        <section class="card" aria-labelledby="recent-tasks">
          <div class="card-head">
            <h2 id="recent-tasks"><Icon name="tasks" />Recent tasks</h2>
            <RouterLink to="/tasks">All tasks</RouterLink>
          </div>
          <div v-if="loading && !tasks.length" class="list">
            <div v-for="i in 4" :key="i" class="list-row"><span class="skel" style="width: 60%" /></div>
          </div>
          <EmptyState v-else-if="!tasks.length" title="No tasks yet" icon="tasks" compact>
            Tasks run autonomously on an agent and stream their transcript here.
            <template v-if="auth.isOperator" #actions><RouterLink to="/tasks?new=1" class="btn btn-primary btn-sm"><Icon name="plus" />New task</RouterLink></template>
          </EmptyState>
          <div v-else class="table-wrap">
            <table class="table">
              <thead>
                <tr><th>Task</th><th>Status</th><th class="right hide-mobile">Cost</th><th class="right hide-mobile">Duration</th></tr>
              </thead>
              <tbody>
                <tr v-for="t in tasks" :key="t.id" class="clickable" tabindex="0" @click="router.push(`/tasks/${t.id}`)" @keydown.enter="router.push(`/tasks/${t.id}`)">
                  <td style="max-width: 360px">
                    <RouterLink :to="`/tasks/${t.id}`" class="cell-title truncate" style="display: block" @click.stop>{{ t.title || t.goal }}</RouterLink>
                    <div class="cell-sub truncate">{{ catalog.agentName(t.assigned_agent_id ?? t.agent_id) }} · {{ relTime(t.updated_at, now) }}</div>
                  </td>
                  <td><Badge :value="t.status" /></td>
                  <td class="right num hide-mobile">{{ usd(t.cost_usd) }}</td>
                  <td class="right num nowrap hide-mobile">{{ taskDuration(t) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>
      </div>

      <div class="stack loose">
      <!-- attention -->
      <section class="card" aria-labelledby="attention-title">
        <div class="card-head">
          <h2 id="attention-title"><Icon name="bell" />Needs your attention</h2>
          <RouterLink to="/approvals">Inbox</RouterLink>
        </div>
        <div v-if="loading && !approvals.length" class="list">
          <div v-for="i in 2" :key="i" class="list-row"><span class="skel" style="width: 70%" /></div>
        </div>
        <EmptyState v-else-if="!approvals.length" title="You're all caught up" icon="checkCircle" compact>No tool calls or change plans are waiting for a decision.</EmptyState>
        <div v-else class="list">
          <div v-for="a in approvals" :key="a.id" class="list-row" style="flex-direction: column; align-items: stretch; gap: 8px">
            <div class="row between">
              <div class="row" style="min-width: 0">
                <span v-if="planOf(a)" class="strong truncate"><Icon name="clipboard" :size="14" style="vertical-align: -2px" /> Change plan</span>
                <span v-else class="mono strong truncate">{{ a.tool }}</span>
                <Badge :value="a.risk" kind="risk" />
              </div>
              <span class="countdown" :class="{ urgent: (new Date(a.expires_at).getTime() - now) < 60000 }">
                <Icon name="clock" />{{ countdown(a.expires_at, now) }}
              </span>
            </div>
            <PlanView v-if="planOf(a)" :plan="planOf(a)!" compact />
            <div class="small muted truncate">
              <RouterLink :to="`/agents/${a.agent_id}`">{{ catalog.agentName(a.agent_id) }}</RouterLink>
              <template v-if="!planOf(a)"> · <span class="mono">{{ approvalSummary(a) }}</span></template>
            </div>
            <div class="row">
              <template v-if="auth.isOperator">
                <button type="button" class="btn btn-primary btn-sm" :disabled="deciding === a.id" @click="decide(a, true)"><Icon name="check" />{{ planOf(a) ? 'Approve plan' : 'Approve' }}</button>
                <button type="button" class="btn btn-sm" :disabled="deciding === a.id" @click="decide(a, false)">Deny</button>
              </template>
              <RouterLink v-if="pendingChanges.get(a.id)" :to="`/changes/${pendingChanges.get(a.id)!.id}`" class="small" style="margin-left: auto">Review plan<Icon name="arrowRight" :size="13" style="vertical-align: -2px; margin-left: 2px" /></RouterLink>
              <RouterLink v-else :to="`/sessions/${a.session_id}`" class="small" style="margin-left: auto">Context<Icon name="arrowRight" :size="13" style="vertical-align: -2px; margin-left: 2px" /></RouterLink>
            </div>
          </div>
        </div>
      </section>

      <!-- coding -->
      <section v-if="coder.projects.length" class="card" aria-labelledby="coding-title">
        <div class="card-head">
          <h2 id="coding-title"><Icon name="gitPR" />Pull requests · 7 days</h2>
          <RouterLink to="/projects">Projects</RouterLink>
        </div>
        <EmptyState v-if="!prTasks.length" title="No pull requests this week" icon="gitPR" compact>
          Coding tasks on your {{ coder.projects.length }} project{{ coder.projects.length > 1 ? 's' : '' }} open pull requests here.
        </EmptyState>
        <div v-else class="list">
          <div v-for="t in prTasks.slice(0, 5)" :key="t.id" class="list-row">
            <div class="grow" style="min-width: 0">
              <RouterLink :to="`/tasks/${t.id}`" class="strong truncate" style="display: block; color: var(--text-primary)">{{ t.title || t.goal }}</RouterLink>
              <div class="small muted truncate">{{ coder.project(t.project_id)?.name ?? 'project' }} · {{ relTime(t.created_at, now) }}</div>
            </div>
            <PrLink :url="t.pr_url" :number="t.pr_number" />
          </div>
          <div v-if="prTasks.length > 5" class="list-row small muted">and {{ prTasks.length - 5 }} more on the <RouterLink to="/projects">projects</RouterLink></div>
        </div>
      </section>
      </div>
    </div>
  </div>
</template>
