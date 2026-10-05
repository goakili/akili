<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { api, ApiError, AUTONOMY_LEVELS, type Change, type Task, type TaskPlan } from '../api'
import { useAuth } from '../stores/auth'
import { useCatalog } from '../stores/catalog'
import { useCoder } from '../stores/coder'
import { useConfirm } from '../stores/confirm'
import { useLive } from '../stores/live'
import { useToast } from '../stores/toast'
import { useUi } from '../stores/ui'
import { durationSec, fmtDate, fmtTime, usd, duration, safeUrl } from '../lib/format'
import { copyText } from '../lib/clipboard'
import { prNoun, prNumber } from '../lib/forge'
import { useNow } from '../lib/now'
import { fetchAll } from '../lib/paged'
import Badge from '../components/Badge.vue'
import SafeMarkdown from '../components/SafeMarkdown'
import SessionTranscript from '../components/SessionTranscript.vue'
import PageHeader from '../components/PageHeader.vue'
import EmptyState from '../components/EmptyState.vue'
import DiffViewer from '../components/DiffViewer.vue'
import PrLink from '../components/PrLink.vue'
import TriggerChip from '../components/TriggerChip.vue'
import Icon, { type IconName } from '../components/Icon'

const props = defineProps<{ id: string }>()
const auth = useAuth()
const catalog = useCatalog()
const coder = useCoder()
const confirm = useConfirm()
const live = useLive()
const toast = useToast()
const ui = useUi()
const router = useRouter()
const now = useNow()

const task = ref<Task | null>(null)
/** Change plans proposed while working on this task. */
const changes = ref<Change[]>([])
function loadChanges() {
  fetchAll((page) => api.pageChanges({ task_id: props.id, page, size: 200 }, { quiet: true })).then((c) => (changes.value = c)).catch(() => {})
}
const notFound = ref(false)
const busy = ref(false)

const continuable = computed(() => !!task.value?.session_id && ['failed', 'cancelled', 'timed_out'].includes(task.value.status))
const terminal = computed(() => !!task.value && ['succeeded', 'failed', 'cancelled', 'timed_out'].includes(task.value.status))

function setTask(t: Task) {
  task.value = t
  ui.crumb = t.title || 'Task'
}

// The plans as the agent received them (snapshots taken when the task was created).
const linkedPlans = ref<TaskPlan[]>([])
function loadPlans() {
  api.taskPlans(props.id, { quiet: true }).then((p) => (linkedPlans.value = p ?? [])).catch(() => {})
}

async function start() {
  busy.value = true
  try {
    setTask(await api.startTask(props.id))
    toast.success('Task queued')
  } catch {
    /* toasted */
  } finally {
    busy.value = false
  }
}

async function load() {
  loadPlans()
  try {
    setTask(await api.getTask(props.id, { quiet: true }))
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound.value = true
    else if (e instanceof ApiError) toast.error(e.message)
  }
}

function onTask(t: Task) {
  if (t.id === props.id) setTask(t)
}

async function cancel() {
  const ok = await confirm.ask({
    title: `Cancel “${task.value?.title || 'this task'}”?`,
    message: 'The agent is interrupted and the task marked cancelled.',
    confirmText: 'Cancel task',
    danger: true,
  })
  if (!ok) return
  busy.value = true
  try {
    setTask(await api.cancelTask(props.id))
    toast.success('Task cancelled')
  } catch {
    /* toasted */
  } finally {
    busy.value = false
  }
}

async function resume() {
  busy.value = true
  try {
    setTask(await api.continueTask(props.id))
    toast.success('Task continues from where it stopped')
  } catch {
    /* toasted */
  } finally {
    busy.value = false
  }
}

async function retry() {
  busy.value = true
  try {
    const t = await api.retryTask(props.id)
    toast.success('Task queued again')
    if (t.id !== props.id) router.push(`/tasks/${t.id}`)
    else setTask(t)
  } catch {
    /* toasted */
  } finally {
    busy.value = false
  }
}

const target = computed(() => {
  const t = task.value
  if (!t) return ''
  if (t.agent_id) return catalog.agentName(t.agent_id)
  if (t.selector?.length) return `labels: ${t.selector.join(', ')}`
  return 'any agent'
})

interface Step {
  label: string
  time: string
  state: 'done' | 'current' | 'failed' | ''
  icon: IconName
}
const steps = computed<Step[]>(() => {
  const t = task.value
  if (!t) return []
  const s = t.status
  const assigned = !!t.assigned_agent_id || !!t.started_at || s === 'assigned' || s === 'running'
  const started = !!t.started_at || s === 'running'
  const ok = s === 'succeeded'
  const finishLabel: Record<string, string> = { succeeded: 'Succeeded', failed: 'Failed', cancelled: 'Cancelled', timed_out: 'Timed out' }
  return [
    { label: 'Created', time: fmtTime(t.created_at), state: 'done', icon: 'check' },
    {
      label: 'Assigned',
      time: t.assigned_agent_id ? catalog.agentName(t.assigned_agent_id) : s === 'queued' ? 'waiting for an agent' : '',
      state: assigned ? 'done' : s === 'queued' ? 'current' : '',
      icon: assigned ? 'check' : s === 'queued' ? 'loader' : 'circle',
    },
    {
      label: 'Running',
      time: t.started_at ? fmtTime(t.started_at) : '',
      state: started ? (terminal.value ? 'done' : 'current') : s === 'assigned' ? 'current' : '',
      icon: started && terminal.value ? 'check' : started || s === 'assigned' ? 'loader' : 'circle',
    },
    {
      label: terminal.value ? finishLabel[s] : 'Finished',
      time: t.finished_at ? fmtTime(t.finished_at) : '',
      state: terminal.value ? (ok ? 'done' : 'failed') : '',
      icon: terminal.value ? (ok ? 'check' : 'x') : 'circle',
    },
  ]
})

const elapsed = computed(() => {
  const t = task.value
  if (!t?.started_at) return ''
  const end = t.finished_at ? new Date(t.finished_at).getTime() : now.value
  return duration(Math.max(0, end - new Date(t.started_at).getTime()))
})

// ---- code (coding tasks) -------------------------------------------------------------------------

const project = computed(() => coder.project(task.value?.project_id))
const repoUrl = computed(() => safeUrl(project.value?.web_url))
const diffText = ref<string | null>(null)
const diffLoading = ref(false)
const diffError = ref('')
const branchCopied = ref(false)

async function loadDiff() {
  if (!task.value?.pr_number) return
  diffLoading.value = true
  diffError.value = ''
  try {
    diffText.value = await api.taskDiff(props.id, { quiet: true })
  } catch (e) {
    diffError.value = e instanceof ApiError ? e.message : 'The diff could not be loaded.'
  } finally {
    diffLoading.value = false
  }
}

// Fetch the diff once a PR exists, and again when the task finishes (later pushes change it).
watch(
  () => [task.value?.pr_number ?? 0, terminal.value] as const,
  ([pr, done], old) => {
    if (!pr) return
    if (diffText.value === null && !diffLoading.value) loadDiff()
    else if (old && done && !old[1]) loadDiff()
  },
)

async function copyBranch() {
  if (!task.value?.branch) return
  branchCopied.value = await copyText(task.value.branch)
  setTimeout(() => (branchCopied.value = false), 1500)
}

const TRIGGERS: Record<string, string> = { manual: 'Manual', schedule: 'Schedule', issue: 'Issue', template: 'Project template', alert: 'Alert', miabi: 'Miabi' }

let off: (() => void) | null = null
let offRe: (() => void) | null = null
onMounted(() => {
  load()
  catalog.loadAgents()
  coder.loadProjects()
  coder.loadIntegrations()
  loadChanges()
  off = live.on((ev) => {
    if (ev.type === 'task.updated' && ev.task_id === props.id && ev.data) setTask(ev.data as Task)
    if (ev.type === 'change.updated' && ev.task_id === props.id && ev.data) {
      const c = ev.data as Change
      const i = changes.value.findIndex((x) => x.id === c.id)
      if (i >= 0) changes.value[i] = c
      else changes.value.unshift(c)
    }
  })
  offRe = live.onReconnect(load)
})
onUnmounted(() => {
  off?.()
  offRe?.()
})
</script>

<template>
  <EmptyState v-if="notFound" title="Task not found" icon="tasks">
    It may have been removed.
    <template #actions><RouterLink to="/tasks" class="btn">Back to tasks</RouterLink></template>
  </EmptyState>
  <div v-else-if="!task" class="stack loose" aria-busy="true">
    <span class="skel lg" style="width: 320px" />
    <div class="skel skel-card" />
  </div>
  <div v-else class="stack loose">
    <PageHeader :title="task.title || 'Task'" :back="{ to: '/tasks', label: 'Tasks' }" :subtitle="task.status_reason || undefined" style="margin-bottom: 0">
      <template #badges><Badge :value="task.status" /><TriggerChip :task="task" /></template>
      <template v-if="auth.isOperator">
        <button v-if="task.status === 'draft'" type="button" class="btn btn-primary" :disabled="busy" @click="start"><Icon name="play" />Start task</button>
        <button v-if="!terminal" type="button" class="btn btn-danger-ghost" :disabled="busy" @click="cancel"><Icon name="stop" />{{ task.status === 'draft' ? 'Discard draft' : 'Cancel task' }}</button>
        <button v-if="continuable" type="button" class="btn btn-primary" :disabled="busy" title="Run again on the same agent, starting from this run's conversation" @click="resume"><Icon name="play" />Continue</button>
        <button v-if="terminal" type="button" class="btn" :class="{ 'btn-primary': !continuable }" :disabled="busy" title="Start over as a new task" @click="retry"><Icon name="refresh" />Retry</button>
      </template>
    </PageHeader>

    <section class="card" aria-label="Progress">
      <div class="card-body">
        <ol class="steps" style="list-style: none; margin: 0; padding: 0">
          <li v-for="s in steps" :key="s.label" class="step" :class="s.state">
            <span class="dot" aria-hidden="true"><Icon :name="s.icon" /></span>
            <span class="s-label">{{ s.label }}<span class="sr-only">{{ s.state === 'done' ? ' (done)' : s.state === 'current' ? ' (in progress)' : s.state === 'failed' ? ' (failed)' : ' (not reached)' }}</span></span>
            <span class="s-time">{{ s.time || '—' }}</span>
          </li>
        </ol>
      </div>
    </section>

    <section v-if="task.project_id" class="card" aria-labelledby="code-title">
      <div class="card-head">
        <h2 id="code-title"><Icon name="gitBranch" />Code</h2>
        <PrLink v-if="task.pr_url" :url="task.pr_url" :number="task.pr_number" button :label="`${prNoun(task.pr_url).replace(/^./, (c) => c.toUpperCase())} ${prNumber(task.pr_url, task.pr_number)}`" />
      </div>
      <div class="card-body stack">
        <dl class="kv code-kv">
          <dt>Repository</dt>
          <dd>
            <template v-if="project">
              <RouterLink :to="`/projects/${project.id}`">{{ project.name }}</RouterLink>
              <span class="muted"> · </span>
              <a v-if="repoUrl" :href="repoUrl" target="_blank" rel="noopener noreferrer" class="mono small">{{ project.owner }}/{{ project.repo }}<Icon name="external" :size="12" style="vertical-align: -1px; margin-left: 2px" /></a>
              <span v-else class="mono small">{{ project.owner }}/{{ project.repo }}</span>
            </template>
            <span v-else class="muted mono small">{{ task.project_id }}</span>
          </dd>
          <dt>Branch</dt>
          <dd>
            <span v-if="task.branch" class="row" style="gap: 6px; display: inline-flex">
              <code class="label-chip" style="margin: 0">{{ task.branch }}</code>
              <button type="button" class="btn btn-xs btn-ghost" :aria-label="branchCopied ? 'Copied' : 'Copy branch name'" @click="copyBranch"><Icon :name="branchCopied ? 'check' : 'copy'" />{{ branchCopied ? 'Copied' : 'Copy' }}</button>
              <span v-if="project" class="small muted nowrap">→ {{ project.default_branch }}</span>
            </span>
            <span v-else class="muted">not created yet</span>
          </dd>
          <dt>Pull request</dt>
          <dd>
            <PrLink v-if="task.pr_url" :url="task.pr_url" :number="task.pr_number" />
            <span v-else class="muted">{{ terminal ? 'none was opened' : 'not opened yet' }}</span>
          </dd>
          <template v-if="task.trigger && task.trigger !== 'manual'">
            <dt>Trigger</dt>
            <dd>{{ TRIGGERS[task.trigger] ?? task.trigger }}<span v-if="task.trigger_ref" class="mono small muted"> · {{ task.trigger_ref }}</span></dd>
          </template>
        </dl>

        <template v-if="task.pr_number > 0">
          <div v-if="diffError" class="banner danger" role="alert">
            <Icon name="alert" /><div class="banner-body">{{ diffError }}</div>
            <button type="button" class="btn btn-sm" @click="loadDiff"><Icon name="refresh" />Retry</button>
          </div>
          <div v-else-if="diffText === null" class="stack tight" aria-busy="true"><span class="skel" style="width: 40%" /><div class="skel skel-card" /></div>
          <DiffViewer v-else :text="diffText">
            <template #actions>
              <button type="button" class="btn btn-xs btn-ghost" :disabled="diffLoading" title="Fetch the pull request diff again" @click="loadDiff">
                <span v-if="diffLoading" class="spinner" style="width: 12px; height: 12px" /><Icon v-else name="refresh" />Refresh
              </button>
            </template>
          </DiffViewer>
        </template>
        <p v-else class="small muted" style="margin: 0"><Icon name="gitDiff" :size="14" style="vertical-align: -2px" /> The diff appears here once the agent opens a pull request.</p>
      </div>
    </section>

    <div class="detail-grid">
      <section class="card">
        <div class="card-head"><h2><Icon name="sparkles" />Goal</h2></div>
        <div class="card-body stack">
          <div class="pre-wrap">{{ task.goal }}</div>
          <div v-if="task.status === 'draft'" class="banner info" role="note">
            <Icon name="info" />
            <div class="banner-body">This task is a <strong>draft</strong>: no agent works on it until you start it.</div>
          </div>
          <div v-if="linkedPlans.length">
            <div class="section-title" style="margin-bottom: 8px">Plans</div>
            <ul class="linked-plans">
              <li v-for="lp in linkedPlans" :key="lp.plan_id">
                <RouterLink :to="{ path: `/projects/${task.project_id}`, query: { tab: 'plans' } }"><Icon name="list" :size="13" />{{ lp.snapshot.title }}</RouterLink>
                <span v-if="lp.phase_id" class="small">
                  phase: <strong>{{ (lp.snapshot.phases ?? []).find((p) => p.id === lp.phase_id)?.title ?? lp.phase_id }}</strong>
                </span>
                <span class="small muted">
                  {{ (lp.snapshot.phases ?? []).filter((s) => s.status === 'done' || s.status === 'skipped').length }}/{{ (lp.snapshot.phases ?? []).length }} phases done when the task was created
                </span>
              </li>
            </ul>
          </div>
          <div v-if="task.result">
            <div class="section-title" style="margin-bottom: 8px">Result</div>
            <div class="banner ok" style="display: block"><SafeMarkdown :text="task.result" /></div>
          </div>
          <div v-if="task.error">
            <div class="section-title" style="margin-bottom: 8px">Error</div>
            <pre class="code error">{{ task.error }}</pre>
          </div>
        </div>
      </section>
      <section class="card">
        <div class="card-head"><h2><Icon name="info" />Details</h2></div>
        <div class="card-body">
          <dl class="kv">
            <dt>Target</dt><dd>{{ target }}</dd>
            <dt>Assigned to</dt>
            <dd>
              <RouterLink v-if="task.assigned_agent_id" :to="`/agents/${task.assigned_agent_id}`">{{ catalog.agentName(task.assigned_agent_id) }}</RouterLink>
              <span v-else class="muted">not yet</span>
            </dd>
            <dt>Autonomy</dt><dd>{{ AUTONOMY_LEVELS[task.autonomy]?.label ?? `L${task.autonomy}` }}</dd>
            <dt>Cost</dt><dd class="num">{{ usd(task.cost_usd) }} <span class="muted">of {{ task.budget_usd ? usd(task.budget_usd) : 'no cap' }}</span></dd>
            <dt>Duration</dt><dd class="num">{{ elapsed || '—' }}</dd>
            <dt>Attempts</dt><dd>{{ task.attempts }} of {{ task.max_attempts }}</dd>
            <dt>Limits</dt><dd>{{ task.max_turns }} turns · {{ durationSec(task.timeout_sec) }} timeout</dd>
            <dt>Priority</dt><dd>{{ task.priority }}</dd>
            <dt>Created</dt><dd>{{ fmtDate(task.created_at) }}</dd>
            <template v-if="task.schedule_id"><dt>Schedule</dt><dd><RouterLink to="/schedules">View schedules</RouterLink></dd></template>
            <template v-if="task.trigger === 'alert' || task.trigger === 'miabi'">
              <dt>Trigger</dt>
              <dd class="row wrap" style="gap: 6px"><TriggerChip :task="task" /><span class="mono xs muted truncate" style="max-width: 260px" :title="task.trigger_ref">{{ task.trigger_ref }}</span></dd>
            </template>
            <template v-if="changes.length">
              <dt>Changes</dt>
              <dd class="stack tight">
                <RouterLink v-for="c in changes" :key="c.id" :to="`/changes/${c.id}`" class="row" style="gap: 6px"><Badge :value="c.status" />{{ c.title }}</RouterLink>
              </dd>
            </template>
            <template v-if="task.session_id"><dt>Session</dt><dd><RouterLink :to="`/sessions/${task.session_id}`">Open full transcript</RouterLink></dd></template>
            <dt>ID</dt><dd class="mono small">{{ task.id }}</dd>
          </dl>
        </div>
      </section>
    </div>

    <section v-if="task.session_id" class="stack tight" aria-labelledby="transcript-title">
      <h2 id="transcript-title">Transcript</h2>
      <SessionTranscript :key="task.session_id" :session-id="task.session_id" class="task-frame" @task="onTask" />
    </section>
    <div v-else class="card">
      <EmptyState title="Waiting for an agent" icon="hourglass" compact>The transcript appears here once an agent picks up the task.</EmptyState>
    </div>
  </div>
</template>

<style scoped>
.linked-plans {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.linked-plans a {
  display: inline-flex;
  align-items: center;
  gap: 5px;
}
.linked-plans li {
  display: flex;
  gap: 8px;
  align-items: baseline;
  flex-wrap: wrap;
}
.task-frame {
  height: 680px;
  max-height: calc(100vh - 120px);
  min-height: 420px;
}
</style>
