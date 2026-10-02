<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, ApiError, AUTONOMY_LEVELS, type Agent, type AuditLog, type ChatSession, type Enrollment, type TerminalSession, type User } from '../api'
import { useAuth } from '../stores/auth'
import { useCatalog } from '../stores/catalog'
import { useConfirm } from '../stores/confirm'
import { useLive } from '../stores/live'
import { useToast } from '../stores/toast'
import { useUi } from '../stores/ui'
import { agentFormFrom, agentInput, type AgentForm } from '../lib/agentForm'
import { fmtDate, relTime, usd, num, durationSec, duration } from '../lib/format'
import { useNow } from '../lib/now'
import Badge from '../components/Badge.vue'
import Modal from '../components/Modal.vue'
import AgentFields from '../components/AgentFields.vue'
import EnrollmentPanel from '../components/EnrollmentPanel.vue'
import PageHeader from '../components/PageHeader.vue'
import EmptyState from '../components/EmptyState.vue'
import SkeletonRows from '../components/SkeletonRows.vue'
import Icon, { type IconName } from '../components/Icon'

const props = defineProps<{ id: string }>()
const auth = useAuth()
const catalog = useCatalog()
const confirm = useConfirm()
const live = useLive()
const toast = useToast()
const ui = useUi()
const route = useRoute()
const router = useRouter()
const now = useNow()

const agent = ref<Agent | null>(null)
const sessions = ref<ChatSession[] | null>(null)
const activity = ref<AuditLog[] | null>(null)
const terminals = ref<TerminalSession[] | null>(null)
const users = ref<User[]>([])
const notFound = ref(false)
const form = ref<AgentForm>(agentFormFrom())
const initial = ref<AgentForm>(agentFormFrom())
const saving = ref(false)
const busy = ref(false)
const enrollment = ref<Enrollment | null>(null)

type TabId = 'overview' | 'config' | 'sessions' | 'terminals' | 'activity'
const TABS = computed(() => {
  const t: { id: TabId; label: string; icon: IconName; count?: number }[] = [
    { id: 'overview', label: 'Overview', icon: 'overview' },
    { id: 'config', label: 'Configuration', icon: 'settings' },
    { id: 'sessions', label: 'Sessions', icon: 'chat', count: sessions.value?.length },
  ]
  if (auth.isAdmin) t.push({ id: 'terminals', label: 'Terminal sessions', icon: 'terminal', count: terminals.value?.length })
  if (auth.isAdmin) t.push({ id: 'activity', label: 'Activity', icon: 'activity' })
  return t
})
const tab = computed<TabId>(() => (TABS.value.find((t) => t.id === route.query.tab)?.id ?? 'overview'))
function setTab(id: TabId) {
  router.replace({ query: { ...route.query, tab: id === 'overview' ? undefined : id } })
}

const dirty = computed(() => Object.keys(agentInput(form.value, initial.value)).length > 0)
const canChat = computed(() => auth.isOperator && agent.value && agent.value.status !== 'pending' && agent.value.status !== 'revoked')

async function load(resetForm = true) {
  try {
    const a = await api.getAgent(props.id)
    agent.value = a
    ui.crumb = a.name
    catalog.upsertAgent(a)
    if (resetForm || !dirty.value) {
      form.value = agentFormFrom(a)
      initial.value = agentFormFrom(a)
    }
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound.value = true
  }
  loadSessions()
}

function loadSessions() {
  api.listSessions({ agent_id: props.id, limit: 50 }).then((s) => (sessions.value = s ?? [])).catch(() => (sessions.value = []))
}

async function loadActivity() {
  if (!auth.isAdmin) return
  try {
    const r = await api.audit({ target_id: props.id, size: 50 })
    activity.value = r.items ?? []
  } catch {
    activity.value = []
  }
}
watch(tab, (t) => t === 'activity' && activity.value === null && loadActivity(), { immediate: true })

async function loadTerminals() {
  if (!auth.isAdmin) return
  try {
    const [t, u] = await Promise.all([api.listTerminals({ agent_id: props.id }, { quiet: true }), users.value.length ? null : api.listUsers().catch(() => null)])
    terminals.value = t ?? []
    if (u) users.value = u
  } catch {
    terminals.value = []
  }
}
watch(tab, (t) => t === 'terminals' && loadTerminals(), { immediate: true })

function userName(id: string): string {
  const u = users.value.find((x) => x.id === id)
  return u ? u.name || u.email : id
}

function termDuration(t: TerminalSession): string {
  if (!t.ended_at) return t.status === 'open' ? 'open' : '—'
  return duration(new Date(t.ended_at).getTime() - new Date(t.created_at).getTime())
}

function bytes(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

// Terminal: admins, agent online, and the bound policy allows it. Say why when not.
const terminalBlock = computed(() => {
  const a = agent.value
  if (!a) return 'Loading…'
  if (a.status !== 'online') return `${a.name} is ${a.status}: a terminal needs the agent connected.`
  if (!a.policy_id) return 'No policy is bound; only a policy with “terminal” enabled allows terminals.'
  const p = catalog.agentPolicy(a)
  if (!p) return 'Checking the agent’s policy…'
  if (!p.document.terminal) return `The ${p.name} policy does not allow terminals. Enable “Terminal” in a policy and bind it to this agent.`
  return ''
})

async function save() {
  const body = agentInput(form.value, initial.value)
  if (!Object.keys(body).length || !form.value.name.trim()) return
  saving.value = true
  try {
    const a = await api.updateAgent(props.id, body)
    agent.value = a
    ui.crumb = a.name
    catalog.upsertAgent(a)
    form.value = agentFormFrom(a)
    initial.value = agentFormFrom(a)
    toast.success('Agent configuration saved')
  } catch {
    /* toasted */
  } finally {
    saving.value = false
  }
}

async function chat() {
  busy.value = true
  try {
    const s = await api.createSession(props.id)
    router.push(`/sessions/${s.id}`)
  } catch {
    /* toasted */
  } finally {
    busy.value = false
  }
}

async function toggleDrain() {
  if (!agent.value) return
  const draining = !agent.value.draining
  busy.value = true
  try {
    await api.drainAgent(props.id, draining)
    agent.value.draining = draining
    toast.success(draining ? 'Draining: no new tasks will be scheduled' : 'Agent is accepting tasks again')
  } catch {
    /* toasted */
  } finally {
    busy.value = false
  }
}

async function reenroll() {
  const ok = await confirm.ask({
    title: `Re-enroll ${agent.value?.name}?`,
    message: 'This revokes the agent’s current key immediately and issues a new one-time join token. The agent disconnects until it enrolls again.',
    confirmText: 'Re-enroll',
    danger: true,
  })
  if (!ok) return
  busy.value = true
  try {
    enrollment.value = await api.reenrollAgent(props.id)
    await load(false)
  } catch {
    /* toasted */
  } finally {
    busy.value = false
  }
}

async function revoke() {
  const ok = await confirm.ask({
    title: `Revoke ${agent.value?.name}?`,
    message: 'The agent is disconnected and can never reconnect with its current identity. Use re-enroll to bring it back later.',
    confirmText: `Revoke ${agent.value?.name}`,
    danger: true,
  })
  if (!ok) return
  busy.value = true
  try {
    await api.revokeAgent(props.id)
    toast.success(`${agent.value?.name} revoked`)
    await load(false)
  } catch {
    /* toasted */
  } finally {
    busy.value = false
  }
}

async function remove() {
  const name = agent.value?.name ?? ''
  const ok = await confirm.ask({
    title: `Delete ${name}?`,
    message: 'The agent is revoked and removed. Its sessions and audit history remain.',
    confirmText: `Delete ${name}`,
    danger: true,
    requireText: name,
  })
  if (!ok) return
  busy.value = true
  try {
    await api.deleteAgent(props.id)
    toast.success(`${name} deleted`)
    catalog.loadAgents(true)
    router.replace('/agents')
  } catch {
    busy.value = false
  }
}

// Hosts often report many addresses; show IPv4 first and summarise the rest.
function ips(list?: string[]): string {
  const all = [...(list ?? [])].sort((a, b) => Number(a.includes(':')) - Number(b.includes(':')))
  if (!all.length) return '—'
  const shown = all.slice(0, 3).join(', ')
  return all.length > 3 ? `${shown} +${all.length - 3} more` : shown
}

function mem(mb?: number) {
  if (!mb) return '—'
  return mb >= 1024 ? `${(mb / 1024).toFixed(1)} GB` : `${mb} MB`
}

const facts = computed(() => {
  const f = agent.value?.facts
  const out: { label: string; icon: IconName; value: string; mono?: boolean; title?: string }[] = [
    { label: 'Hostname', icon: 'server', value: f?.hostname || '—' },
    { label: 'OS / arch', icon: 'monitor', value: [f?.os, f?.arch].filter(Boolean).join(' / ') || '—' },
    { label: 'CPUs', icon: 'cpu', value: f?.cpus ? String(f.cpus) : '—' },
    { label: 'Memory free', icon: 'gauge', value: f?.mem_total_mb ? `${mem(f?.mem_avail_mb)} of ${mem(f?.mem_total_mb)}` : '—' },
    { label: 'Load (1m)', icon: 'activity', value: f?.load1 != null ? String(f.load1) : '—' },
    { label: 'Uptime', icon: 'clock', value: durationSec(f?.uptime_sec) },
    { label: 'Kernel', icon: 'terminal', value: f?.kernel || '—', mono: true },
    { label: 'IPs', icon: 'globe', value: ips(f?.ips), title: (f?.ips ?? []).join('\n'), mono: true },
    { label: 'Workdir', icon: 'folder', value: f?.workdir || '—', mono: true },
    { label: 'Agent version', icon: 'layers', value: agent.value?.version || f?.agent_version || '—', mono: true },
  ]
  return out
})

let off: (() => void) | null = null
onMounted(() => {
  load()
  catalog.loadPolicies().catch(() => {})
  off = live.on((ev) => {
    if (ev.agent_id !== props.id) return
    if (ev.type === 'agent.status') load(false)
    if (ev.type === 'session.created' || ev.type === 'session.closed') loadSessions()
  })
})
onUnmounted(() => off?.())
</script>

<template>
  <EmptyState v-if="notFound" title="Agent not found" icon="agents">
    It may have been deleted.
    <template #actions><RouterLink to="/agents" class="btn">Back to agents</RouterLink></template>
  </EmptyState>
  <div v-else-if="!agent" class="stack loose" aria-busy="true">
    <span class="skel lg" style="width: 240px" />
    <div class="skel skel-card" style="height: 260px" />
  </div>
  <div v-else>
    <PageHeader :title="agent.name" :back="{ to: '/agents', label: 'Agents' }">
      <template #badges>
        <Badge :value="agent.status" />
        <Badge v-if="agent.draining" value="draining" />
        <span class="badge outline square" :title="AUTONOMY_LEVELS[agent.autonomy]?.help">L{{ agent.autonomy }}</span>
      </template>
      <template #subtitle>
        {{ agent.description || (agent.facts?.hostname ? `Runs on ${agent.facts.hostname}` : 'Not enrolled yet') }}
        <template v-if="agent.last_seen_at"> · last seen {{ relTime(agent.last_seen_at, now) }}</template>
      </template>
      <button v-if="auth.isOperator" type="button" class="btn btn-primary" :disabled="!canChat || busy" @click="chat">
        <Icon name="chat" />New chat
      </button>
      <span v-if="auth.isAdmin" class="tip-wrap" :title="terminalBlock || 'Open a recorded interactive shell on this host'">
        <RouterLink v-if="!terminalBlock" :to="`/agents/${agent.id}/terminal`" class="btn"><Icon name="terminal" />Terminal</RouterLink>
        <button v-else type="button" class="btn" disabled :aria-describedby="'term-why'"><Icon name="terminal" />Terminal</button>
        <span v-if="terminalBlock" id="term-why" class="sr-only">{{ terminalBlock }}</span>
      </span>
      <button v-if="auth.isOperator" type="button" class="btn" :disabled="busy || agent.status === 'revoked'" @click="toggleDrain">
        <Icon :name="agent.draining ? 'play' : 'pause'" />{{ agent.draining ? 'Resume' : 'Drain' }}
      </button>
      <template v-if="auth.isAdmin">
        <button type="button" class="btn" :disabled="busy" @click="reenroll"><Icon name="key" />Re-enroll</button>
        <button type="button" class="btn btn-danger-ghost" :disabled="busy || agent.status === 'revoked'" @click="revoke"><Icon name="ban" />Revoke</button>
        <button type="button" class="btn btn-danger-ghost btn-icon" :disabled="busy" :aria-label="`Delete ${agent.name}`" title="Delete agent" @click="remove"><Icon name="trash" /></button>
      </template>
    </PageHeader>

    <div class="stack" style="margin-bottom: 20px">
      <div v-if="!agent.policy_id && agent.status !== 'revoked'" class="banner warn" role="alert">
        <Icon name="alert" />
        <div class="banner-body">
          <strong>No policy is bound to this agent.</strong>
          <p>Every tool call is denied, so it can only answer from what it already knows. Choose a policy, for example <em>operator-safe</em> for read-only server and Kubernetes checks.</p>
        </div>
        <button v-if="auth.isAdmin && tab !== 'config'" type="button" class="btn btn-sm" @click="setTab('config')">Bind a policy</button>
      </div>
      <div v-if="agent.status === 'pending'" class="banner info">
        <Icon name="info" />
        <div class="banner-body">
          This agent has not enrolled yet. Run the install command on the host with its join token{{ auth.isAdmin ? ', or re-enroll to get a fresh token' : '' }}.
        </div>
      </div>
    </div>

    <div class="tabs" role="tablist" aria-label="Agent sections">
      <button
        v-for="t in TABS"
        :id="`atab-${t.id}`"
        :key="t.id"
        type="button"
        role="tab"
        class="tab"
        :aria-selected="tab === t.id"
        :aria-controls="`apanel-${t.id}`"
        @click="setTab(t.id)"
      >
        <Icon :name="t.icon" />{{ t.label }}<span v-if="t.count" class="count">{{ t.count }}</span>
      </button>
    </div>

    <!-- overview -->
    <div v-if="tab === 'overview'" id="apanel-overview" role="tabpanel" aria-labelledby="atab-overview" class="detail-grid">
      <section class="card">
        <div class="card-head"><h2><Icon name="server" />Host facts</h2></div>
        <div class="card-body">
          <div class="facts">
            <div v-for="f in facts" :key="f.label" class="fact">
              <div class="f-label"><Icon :name="f.icon" />{{ f.label }}</div>
              <div class="f-value" :class="{ mono: f.mono, small: f.mono }" :title="f.title">{{ f.value }}</div>
            </div>
          </div>
        </div>
      </section>
      <div class="stack loose">
        <section class="card">
          <div class="card-head"><h2><Icon name="approvals" />Guardrails</h2></div>
          <div class="card-body">
            <dl class="kv">
              <dt>Policy</dt>
              <dd>
                <RouterLink v-if="agent.policy_id" to="/policies">{{ catalog.policyName(agent.policy_id) }}</RouterLink>
                <span v-else class="text-warn">none: tools denied</span>
              </dd>
              <dt>Autonomy</dt><dd>{{ AUTONOMY_LEVELS[agent.autonomy]?.label }}</dd>
              <dt>Budget</dt><dd>{{ agent.monthly_budget_usd ? `${usd(agent.monthly_budget_usd)} / month` : 'no limit' }}</dd>
              <dt>Parallel</dt><dd>{{ agent.max_parallel }} sessions</dd>
              <template v-if="agent.git_identity">
                <dt>Commits as</dt><dd class="mono">{{ agent.git_identity.name }} &lt;{{ agent.git_identity.email }}&gt;</dd>
              </template>
              <dt>Labels</dt>
              <dd><span v-for="l in agent.labels ?? []" :key="l" class="label-chip">{{ l }}</span><span v-if="!(agent.labels ?? []).length" class="faint">—</span></dd>
            </dl>
          </div>
        </section>
        <section class="card">
          <div class="card-head"><h2><Icon name="clock" />Lifecycle</h2></div>
          <div class="card-body">
            <dl class="kv">
              <dt>Created</dt><dd>{{ fmtDate(agent.created_at) }}</dd>
              <dt>Enrolled</dt><dd>{{ fmtDate(agent.enrolled_at) }}</dd>
              <dt>Last seen</dt><dd>{{ relTime(agent.last_seen_at, now) }}</dd>
              <dt>Revoked</dt><dd>{{ fmtDate(agent.revoked_at) }}</dd>
              <dt>Active sessions</dt><dd>{{ agent.active_sessions }}</dd>
              <dt>ID</dt><dd class="mono small">{{ agent.id }}</dd>
            </dl>
          </div>
        </section>
      </div>
    </div>

    <!-- configuration -->
    <form v-else-if="tab === 'config'" id="apanel-config" role="tabpanel" aria-labelledby="atab-config" class="card" style="max-width: 980px" novalidate @submit.prevent="save">
      <div class="card-head">
        <h2><Icon name="settings" />Configuration</h2>
        <span v-if="!auth.isAdmin" class="badge outline"><Icon name="lock" />Read-only (admin required)</span>
        <span v-else-if="dirty" class="badge warn">Unsaved changes</span>
      </div>
      <div class="card-body">
        <AgentFields
          v-model="form"
          :disabled="!auth.isAdmin || saving"
          :show-errors="true"
          :git-default-email="agent.git_email ? undefined : agent.git_identity?.email"
        />
      </div>
      <div v-if="auth.isAdmin" class="card-foot">
        <button type="button" class="btn" :disabled="!dirty || saving" @click="form = agentFormFrom(agent)">Discard</button>
        <button type="submit" class="btn btn-primary" :disabled="!dirty || saving || !form.name.trim()">
          <span v-if="saving" class="spinner" />{{ saving ? 'Saving…' : 'Save changes' }}
        </button>
      </div>
    </form>

    <!-- sessions -->
    <section v-else-if="tab === 'sessions'" id="apanel-sessions" role="tabpanel" aria-labelledby="atab-sessions" class="card">
      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr><th>Session</th><th>Mode</th><th>State</th><th class="right hide-mobile">Tokens</th><th class="right hide-mobile">Cost</th><th>Last activity</th></tr>
          </thead>
          <tbody>
            <SkeletonRows v-if="sessions === null" :cols="6" :rows="3" />
            <tr v-else-if="!sessions.length">
              <td colspan="6">
                <EmptyState title="No sessions yet" icon="chat">
                  Chats and task runs with this agent appear here.
                  <template v-if="canChat" #actions><button type="button" class="btn btn-primary btn-sm" @click="chat"><Icon name="chat" />Start a chat</button></template>
                </EmptyState>
              </td>
            </tr>
            <tr v-for="s in sessions ?? []" :key="s.id" class="clickable" tabindex="0" @click="router.push(`/sessions/${s.id}`)" @keydown.enter="router.push(`/sessions/${s.id}`)">
              <td><RouterLink :to="`/sessions/${s.id}`" class="cell-title" @click.stop>{{ s.title || (s.mode === 'task' ? 'Task run' : 'Chat') }}</RouterLink></td>
              <td><Badge :value="s.mode" /></td>
              <td><Badge :value="s.status === 'closed' ? 'closed' : s.state || 'idle'" /></td>
              <td class="right num hide-mobile">{{ num(s.input_tokens + s.output_tokens) }}</td>
              <td class="right num hide-mobile">{{ usd(s.cost_usd) }}</td>
              <td class="nowrap">{{ relTime(s.last_activity_at ?? s.updated_at, now) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <!-- terminal sessions -->
    <section v-else-if="tab === 'terminals'" id="apanel-terminals" role="tabpanel" aria-labelledby="atab-terminals" class="card">
      <div class="card-head">
        <h2><Icon name="terminal" />Recorded terminal sessions</h2>
        <RouterLink v-if="!terminalBlock" :to="`/agents/${agent.id}/terminal`" class="btn btn-sm"><Icon name="terminal" />Open terminal</RouterLink>
      </div>
      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr><th>Started</th><th>User</th><th>Duration</th><th class="right hide-mobile">Recorded</th><th>Exit</th><th><span class="sr-only">Replay</span></th></tr>
          </thead>
          <tbody>
            <SkeletonRows v-if="terminals === null" :cols="6" :rows="3" />
            <tr v-else-if="!terminals.length">
              <td colspan="6">
                <EmptyState title="No terminal sessions yet" icon="terminal" compact>Every terminal opened on this agent is recorded (input and output) and can be replayed here.</EmptyState>
              </td>
            </tr>
            <tr
              v-for="t in terminals ?? []"
              :key="t.id"
              class="clickable"
              tabindex="0"
              @click="router.push(`/terminals/${t.id}?agent=${agent.id}`)"
              @keydown.enter="router.push(`/terminals/${t.id}?agent=${agent.id}`)"
            >
              <td class="nowrap" :title="fmtDate(t.created_at)">{{ relTime(t.created_at, now) }}</td>
              <td>{{ userName(t.user_id) }}</td>
              <td class="nowrap num">{{ termDuration(t) }}</td>
              <td class="right num hide-mobile nowrap">
                {{ bytes(t.bytes) }}<span v-if="t.truncated" class="badge warn" style="margin-left: 6px" title="The recording hit its size limit">truncated</span>
              </td>
              <td>
                <Badge v-if="t.status === 'open'" value="open" />
                <span v-else class="badge outline square" :class="{ danger: t.exit_code !== 0 }">{{ t.exit_code }}</span>
              </td>
              <td class="right"><RouterLink :to="`/terminals/${t.id}?agent=${agent.id}`" class="btn btn-xs" @click.stop><Icon name="play" />Replay</RouterLink></td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <!-- activity -->
    <section v-else-if="tab === 'activity'" id="apanel-activity" role="tabpanel" aria-labelledby="atab-activity" class="card">
      <div class="table-wrap">
        <table class="table">
          <thead><tr><th>When</th><th>Action</th><th>Actor</th><th class="hide-mobile">Details</th></tr></thead>
          <tbody>
            <SkeletonRows v-if="activity === null" :cols="4" :rows="4" />
            <tr v-else-if="!activity.length"><td colspan="4"><EmptyState title="No recorded activity" icon="activity" compact>Audit entries that target this agent show up here.</EmptyState></td></tr>
            <tr v-for="r in activity ?? []" :key="r.id">
              <td class="nowrap" :title="fmtDate(r.created_at)">{{ relTime(r.created_at, now) }}</td>
              <td class="mono small strong">{{ r.action }}</td>
              <td><span class="badge outline">{{ r.actor_type }}</span></td>
              <td class="mono xs muted truncate hide-mobile" style="max-width: 420px">{{ r.metadata ? JSON.stringify(r.metadata) : '' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <div class="pager"><span>Latest 50 entries</span><RouterLink :to="{ path: '/audit', query: { target: agent.id } }">Open in audit log</RouterLink></div>
    </section>

    <Modal :open="!!enrollment" title="New join token" wide @close="enrollment = null">
      <EnrollmentPanel v-if="enrollment" :enrollment="enrollment" />
      <template #footer><button type="button" class="btn btn-primary" @click="enrollment = null">Done</button></template>
    </Modal>
  </div>
</template>
