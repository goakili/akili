<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, ApiError, AUTONOMY_LEVELS, type Autonomy, type ChatSession, type MaintenancePreset, type Project, type Schedule, type Task } from '../api'
import { useAuth } from '../stores/auth'
import { useCatalog } from '../stores/catalog'
import { useCoder } from '../stores/coder'
import { useConfirm } from '../stores/confirm'
import { useLive } from '../stores/live'
import { useToast } from '../stores/toast'
import { useUi } from '../stores/ui'
import { projectFormErrors, projectFormFrom, projectInput, type ProjectForm } from '../lib/projectForm'
import { fmtDate, relTime, safeUrl } from '../lib/format'
import { useNow } from '../lib/now'
import Badge from '../components/Badge.vue'
import Modal from '../components/Modal.vue'
import PageHeader from '../components/PageHeader.vue'
import EmptyState from '../components/EmptyState.vue'
import SkeletonRows from '../components/SkeletonRows.vue'
import ProjectFields from '../components/ProjectFields.vue'
import NewTaskModal from '../components/NewTaskModal.vue'
import PrLink from '../components/PrLink.vue'
import Icon, { type IconName } from '../components/Icon'

const props = defineProps<{ id: string }>()
const auth = useAuth()
const catalog = useCatalog()
const coder = useCoder()
const confirm = useConfirm()
const live = useLive()
const toast = useToast()
const ui = useUi()
const route = useRoute()
const router = useRouter()
const now = useNow()

const project = ref<Project | null>(null)
const notFound = ref(false)
const tasks = ref<Task[] | null>(null)
const schedules = ref<Schedule[] | null>(null)
const sessions = ref<ChatSession[] | null>(null)
const presets = ref<MaintenancePreset[]>([])

// ---- tabs ----------------------------------------------------------------------------------------
type TabId = 'overview' | 'tasks' | 'maintenance' | 'chat'
const TABS = computed(() => {
  const t: { id: TabId; label: string; icon: IconName; count?: number }[] = [
    { id: 'overview', label: 'Overview', icon: 'overview' },
    { id: 'tasks', label: 'Tasks', icon: 'tasks', count: tasks.value?.length },
    { id: 'maintenance', label: 'Maintenance', icon: 'schedules', count: schedules.value?.length },
    { id: 'chat', label: 'Chat', icon: 'chat', count: sessions.value?.length },
  ]
  return t
})
const tab = computed<TabId>(() => TABS.value.find((t) => t.id === route.query.tab)?.id ?? 'overview')
function setTab(id: TabId) {
  router.replace({ query: { ...route.query, tab: id === 'overview' ? undefined : id } })
}

// ---- loading -------------------------------------------------------------------------------------
async function load() {
  try {
    const p = await api.getProject(props.id, { quiet: true })
    project.value = p
    ui.crumb = p.name
    coder.upsertProject(p)
    // First load, or no local edits: take the server's settings.
    if (!initial.value || !dirty.value) resetForm()
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound.value = true
    else if (e instanceof ApiError) toast.error(e.message)
    return
  }
  loadTasks()
  loadSchedules()
  loadSessions()
}

async function loadTasks() {
  try {
    tasks.value = (await api.listTasks({ limit: 500, project_id: props.id }, { quiet: true })) ?? []
  } catch {
    tasks.value = tasks.value ?? []
  }
}
async function loadSchedules() {
  try {
    schedules.value = ((await api.listSchedules()) ?? []).filter((s) => s.template?.project_id === props.id)
  } catch {
    schedules.value = schedules.value ?? []
  }
}
async function loadSessions() {
  try {
    sessions.value = (await api.listSessions({ mode: 'chat', limit: 200, project_id: props.id })) ?? []
  } catch {
    sessions.value = sessions.value ?? []
  }
}

const forge = computed(() => coder.forgeOf(project.value))
const integration = computed(() => coder.integrations.find((i) => i.id === project.value?.integration_id))
const repoUrl = computed(() => safeUrl(project.value?.web_url))
const openTasks = computed(() => (tasks.value ?? []).filter((t) => ['queued', 'assigned', 'running'].includes(t.status)).length)
const prCount = computed(() => (tasks.value ?? []).filter((t) => t.pr_url).length)
const target = computed(() => {
  const p = project.value
  if (!p) return ''
  if (p.agent_id) return catalog.agentName(p.agent_id)
  if (p.selector?.length) return `labels: ${p.selector.join(', ')}`
  return 'any agent'
})

// ---- settings form (admin) -----------------------------------------------------------------------
const form = ref<ProjectForm>(projectFormFrom())
const initial = ref('')
const saving = ref(false)
const tried = ref(false)
function resetForm() {
  form.value = projectFormFrom(project.value)
  initial.value = JSON.stringify(projectInput(form.value))
  tried.value = false
}
const dirty = computed(() => !!project.value && JSON.stringify(projectInput(form.value)) !== initial.value)
async function save() {
  tried.value = true
  if (Object.keys(projectFormErrors(form.value)).length) return
  saving.value = true
  try {
    const p = await api.updateProject(props.id, projectInput(form.value))
    project.value = p
    ui.crumb = p.name
    coder.upsertProject(p)
    resetForm()
    toast.success('Project settings saved')
  } catch {
    /* toasted */
  } finally {
    saving.value = false
  }
}

async function remove() {
  const p = project.value
  if (!p) return
  const ok = await confirm.ask({
    title: `Delete project ${p.name}?`,
    message: `Akili forgets the project. The repository ${p.owner}/${p.repo} on the forge, its branches and pull requests are kept, and past tasks keep their history.`,
    confirmText: 'Delete project',
    danger: true,
    requireText: p.name,
  })
  if (!ok) return
  try {
    await api.deleteProject(p.id)
    coder.removeProject(p.id)
    toast.success(`Project ${p.name} deleted`)
    router.push('/projects')
  } catch {
    /* toasted */
  }
}

// ---- new coding task -----------------------------------------------------------------------------
const showTask = ref(false)
const taskInitial = computed(() => ({ project_id: props.id }))
function onTaskCreated(t: Task) {
  showTask.value = false
  router.push(`/tasks/${t.id}`)
}

// ---- maintenance ---------------------------------------------------------------------------------
const presetFor = ref<MaintenancePreset | null>(null)
const pCron = ref('')
const pAutonomy = ref<Autonomy>(2)
const pEnabled = ref(true)
const scheduling = ref(false)
function openPreset(p: MaintenancePreset) {
  presetFor.value = p
  pCron.value = p.cron
  pAutonomy.value = 2
  pEnabled.value = true
}
async function schedulePreset() {
  const p = presetFor.value
  if (!p || !pCron.value.trim()) return
  scheduling.value = true
  try {
    const s = await api.addMaintenance(props.id, { preset: p.id, cron: pCron.value.trim() === p.cron ? undefined : pCron.value.trim(), autonomy: pAutonomy.value, enabled: pEnabled.value })
    schedules.value = [...(schedules.value ?? []), s]
    presetFor.value = null
    toast.success(`${p.name} scheduled`)
  } catch {
    /* toasted */
  } finally {
    scheduling.value = false
  }
}
function scheduledCount(p: MaintenancePreset) {
  const name = `${project.value?.name}: ${p.name}`
  return (schedules.value ?? []).filter((s) => s.name === name).length
}
async function toggleSchedule(s: Schedule) {
  try {
    Object.assign(s, await api.updateSchedule(s.id, { name: s.name, cron: s.cron, enabled: !s.enabled, template: s.template }))
  } catch {
    /* toasted */
  }
}
async function runSchedule(s: Schedule) {
  try {
    const t = await api.runSchedule(s.id)
    toast.success(`Task queued: ${t.title}`)
    loadTasks()
  } catch {
    /* toasted */
  }
}
async function deleteSchedule(s: Schedule) {
  if (!(await confirm.ask({ title: `Delete schedule ${s.name}?`, message: 'Tasks it already created are kept.', confirmText: 'Delete schedule', danger: true }))) return
  try {
    await api.deleteSchedule(s.id)
    schedules.value = (schedules.value ?? []).filter((x) => x.id !== s.id)
    toast.success('Schedule deleted')
  } catch {
    /* toasted */
  }
}
const CRON_HUMAN: Record<string, string> = {
  '0 6 * * 1': 'Mondays at 06:00 UTC',
  '0 7 * * *': 'daily at 07:00 UTC',
  '0 8 * * 3': 'Wednesdays at 08:00 UTC',
  '0 9 1 * *': 'monthly, on the 1st at 09:00 UTC',
  '0 6 * * *': 'daily at 06:00 UTC',
  '0 * * * *': 'hourly',
}
const PRESET_ICON: Record<string, IconName> = { 'dependency-updates': 'layers', 'security-audit': 'approvals', 'test-health': 'activity', 'docs-drift': 'skills' }

// ---- chat ----------------------------------------------------------------------------------------
const chatAgent = ref('')
const chatTitle = ref('')
const starting = ref(false)
const chatable = computed(() =>
  catalog.agents.filter((a) => a.status !== 'pending' && a.status !== 'revoked').sort((a, b) => Number(b.status === 'online') - Number(a.status === 'online') || a.name.localeCompare(b.name)),
)
const chatAgentObj = computed(() => catalog.agents.find((a) => a.id === chatAgent.value))
function pickDefaultAgent() {
  const p = project.value
  const preferred = p?.agent_id ? chatable.value.find((a) => a.id === p.agent_id) : undefined
  const labelled = p?.selector?.length ? chatable.value.find((a) => a.status === 'online' && p.selector!.every((l) => a.labels?.includes(l))) : undefined
  chatAgent.value = preferred?.id ?? labelled?.id ?? chatable.value.find((a) => a.status === 'online')?.id ?? chatable.value[0]?.id ?? ''
}
async function startChat() {
  if (!chatAgent.value || !project.value) return
  starting.value = true
  try {
    const s = await api.createSession(chatAgent.value, chatTitle.value.trim() || `Coding on ${project.value.name}`, props.id)
    router.push(`/sessions/${s.id}`)
  } catch {
    /* toasted */
  } finally {
    starting.value = false
  }
}
function openChatTab() {
  setTab('chat')
}
watch([() => catalog.agents.length, project], () => {
  if (!chatAgent.value) pickDefaultAgent()
})

// ---- live ----------------------------------------------------------------------------------------
let off: (() => void) | null = null
let offRe: (() => void) | null = null
onMounted(async () => {
  catalog.loadAgents()
  coder.loadIntegrations()
  coder.loadTemplates().then((t) => (presets.value = t.presets ?? []))
  await load()
  off = live.on((ev) => {
    if (ev.type === 'task.updated' && ev.data) {
      const t = ev.data as Task
      if (t.project_id !== props.id || !tasks.value) return
      const i = tasks.value.findIndex((x) => x.id === t.id)
      if (i >= 0) tasks.value[i] = t
      else tasks.value.unshift(t)
    }
    if (ev.type === 'session.created' || ev.type === 'session.closed') loadSessions()
  })
  offRe = live.onReconnect(load)
})
onUnmounted(() => {
  off?.()
  offRe?.()
})
</script>

<template>
  <EmptyState v-if="notFound" title="Project not found" icon="repo">
    It may have been deleted.
    <template #actions><RouterLink to="/projects" class="btn">Back to projects</RouterLink></template>
  </EmptyState>
  <div v-else-if="!project" class="stack loose" aria-busy="true">
    <span class="skel lg" style="width: 320px" />
    <div class="skel skel-card" />
  </div>
  <div v-else class="stack loose">
    <PageHeader :title="project.name" :back="{ to: '/projects', label: 'Projects' }" style="margin-bottom: 0">
      <template #badges>
        <span class="badge outline"><Icon :name="forge === 'github' ? 'github' : 'gitea'" />{{ forge === 'github' ? 'GitHub' : 'Gitea' }}</span>
      </template>
      <template #subtitle>
        <span class="repo-line">
          <a v-if="repoUrl" :href="repoUrl" target="_blank" rel="noopener noreferrer" class="mono">{{ project.owner }}/{{ project.repo }}<Icon name="external" :size="13" style="vertical-align: -2px; margin-left: 3px" /></a>
          <span v-else class="mono">{{ project.owner }}/{{ project.repo }}</span>
          <span class="badge outline square"><Icon name="gitBranch" />{{ project.default_branch }}</span>
          <span v-if="project.description" class="muted">{{ project.description }}</span>
        </span>
      </template>
      <template v-if="auth.isOperator">
        <button type="button" class="btn" @click="openChatTab"><Icon name="chat" />Chat</button>
        <button type="button" class="btn btn-primary" @click="showTask = true"><Icon name="plus" />New coding task</button>
      </template>
    </PageHeader>

    <div>
      <div class="tabs" role="tablist" aria-label="Project sections">
        <button
          v-for="t in TABS"
          :id="`ptab-${t.id}`"
          :key="t.id"
          type="button"
          role="tab"
          class="tab"
          :aria-selected="tab === t.id"
          :aria-controls="`ppanel-${t.id}`"
          @click="setTab(t.id)"
        >
          <Icon :name="t.icon" />{{ t.label }}<span v-if="t.count" class="count">{{ t.count }}</span>
        </button>
      </div>

      <!-- overview -->
      <div v-if="tab === 'overview'" id="ppanel-overview" role="tabpanel" aria-labelledby="ptab-overview" class="detail-grid">
        <form v-if="auth.isAdmin" class="card" novalidate @submit.prevent="save">
          <div class="card-head"><h2><Icon name="settings" />Settings</h2><span v-if="dirty" class="badge warn"><Icon name="edit" />unsaved</span></div>
          <div class="card-body"><ProjectFields v-model="form" id-prefix="pe" :show-errors="tried" name-placeholder="Project name" /></div>
          <div class="card-foot row end">
            <button type="button" class="btn btn-danger-ghost" style="margin-right: auto" @click="remove"><Icon name="trash" />Delete project</button>
            <button type="button" class="btn" :disabled="!dirty || saving" @click="resetForm">Discard</button>
            <button type="submit" class="btn btn-primary" :disabled="!dirty || saving"><span v-if="saving" class="spinner" />Save changes</button>
          </div>
        </form>
        <section v-else class="card">
          <div class="card-head"><h2><Icon name="skills" />Conventions</h2></div>
          <div class="card-body">
            <div v-if="project.instructions" class="pre-wrap">{{ project.instructions }}</div>
            <p v-else class="muted" style="margin: 0">No project instructions. Agents follow the repository as they find it.</p>
          </div>
        </section>

        <section class="card">
          <div class="card-head"><h2><Icon name="info" />Summary</h2></div>
          <div class="card-body">
            <dl class="kv">
              <dt>Repository</dt>
              <dd>
                <a v-if="repoUrl" :href="repoUrl" target="_blank" rel="noopener noreferrer" class="mono">{{ project.owner }}/{{ project.repo }}</a>
                <span v-else class="mono">{{ project.owner }}/{{ project.repo }}</span>
              </dd>
              <dt>Default branch</dt><dd class="mono">{{ project.default_branch }} <span class="muted small">(protected: agents push akili/* only)</span></dd>
              <template v-if="integration"><dt>Integration</dt><dd><RouterLink to="/integrations">{{ integration.name }}</RouterLink></dd></template>
              <dt>Runs on</dt><dd>{{ target }}</dd>
              <dt>Sandbox</dt>
              <dd>
                <span v-if="project.sandbox_image" class="badge info square"><Icon name="box" />{{ project.sandbox_image }}</span>
                <span v-else class="muted">disabled (no image)</span>
              </dd>
              <dt>Trigger label</dt>
              <dd>
                <span v-if="project.trigger_label" class="badge violet square"><Icon name="tag" />{{ project.trigger_label }}</span>
                <span v-else class="muted">none (issues do not create tasks)</span>
              </dd>
              <dt>Open tasks</dt><dd class="num">{{ tasks ? openTasks : '…' }}</dd>
              <dt>Pull requests</dt><dd class="num">{{ tasks ? prCount : '…' }} <span class="muted small">opened by tasks</span></dd>
              <dt>Created</dt><dd>{{ fmtDate(project.created_at) }}</dd>
              <dt>ID</dt><dd class="mono small">{{ project.id }}</dd>
            </dl>
          </div>
        </section>
      </div>

      <!-- tasks -->
      <section v-else-if="tab === 'tasks'" id="ppanel-tasks" role="tabpanel" aria-labelledby="ptab-tasks" class="card">
        <div class="card-head">
          <h2><Icon name="tasks" />Coding tasks</h2>
          <button v-if="auth.isOperator" type="button" class="btn btn-sm btn-primary" @click="showTask = true"><Icon name="plus" />New coding task</button>
        </div>
        <div class="table-wrap">
          <table class="table">
            <thead>
              <tr><th>Task</th><th>Status</th><th>Pull request</th><th class="hide-mobile">Branch</th><th class="hide-mobile">Trigger</th><th class="hide-mobile">Updated</th></tr>
            </thead>
            <tbody>
              <SkeletonRows v-if="tasks === null" :cols="6" :rows="3" />
              <tr v-else-if="!tasks.length">
                <td colspan="6">
                  <EmptyState title="No coding tasks yet" icon="gitPR" compact>
                    Describe a change; an agent makes it on its own branch and opens a pull request.
                    <template v-if="auth.isOperator" #actions><button type="button" class="btn btn-primary btn-sm" @click="showTask = true"><Icon name="plus" />New coding task</button></template>
                  </EmptyState>
                </td>
              </tr>
              <tr v-for="t in tasks ?? []" :key="t.id" class="clickable" tabindex="0" @click="router.push(`/tasks/${t.id}`)" @keydown.enter="router.push(`/tasks/${t.id}`)">
                <td style="max-width: 380px">
                  <RouterLink :to="`/tasks/${t.id}`" class="cell-title truncate" style="display: block" @click.stop>{{ t.title || t.goal }}</RouterLink>
                  <div v-if="t.status_reason" class="cell-sub truncate">{{ t.status_reason }}</div>
                </td>
                <td><Badge :value="t.status" /></td>
                <td><PrLink v-if="t.pr_url" :url="t.pr_url" :number="t.pr_number" /><span v-else class="small muted">—</span></td>
                <td class="hide-mobile"><span v-if="t.branch" class="mono small">{{ t.branch }}</span></td>
                <td class="hide-mobile"><span class="badge outline square">{{ t.trigger || 'manual' }}</span></td>
                <td class="nowrap hide-mobile">{{ relTime(t.updated_at, now) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <!-- maintenance -->
      <div v-else-if="tab === 'maintenance'" id="ppanel-maintenance" role="tabpanel" aria-labelledby="ptab-maintenance" class="stack loose">
        <section aria-labelledby="presets-title" class="stack">
          <div>
            <h2 id="presets-title" style="font-size: 15px; margin: 0">Maintenance presets</h2>
            <p class="small muted" style="margin: 4px 0 0">Recurring upkeep an agent does on its own, each ending in a pull request (or a short report when nothing needs changing).</p>
          </div>
          <div class="preset-grid">
            <article v-for="p in presets" :key="p.id" class="card preset">
              <div class="row" style="gap: 10px; align-items: flex-start">
                <span class="preset-ic" aria-hidden="true"><Icon :name="PRESET_ICON[p.id] ?? 'schedules'" /></span>
                <div class="grow" style="min-width: 0">
                  <div class="strong">{{ p.name }}</div>
                  <div class="xs muted" style="margin-top: 3px"><code class="label-chip">{{ p.cron }}</code></div>
                  <div v-if="CRON_HUMAN[p.cron]" class="xs muted">{{ CRON_HUMAN[p.cron] }}</div>
                </div>
              </div>
              <p class="small" style="margin: 0; color: var(--text-secondary)">{{ p.description }}</p>
              <div class="row" style="margin-top: auto">
                <span v-if="scheduledCount(p)" class="badge ok"><Icon name="check" />scheduled</span>
                <button v-if="auth.isOperator" type="button" class="btn btn-sm" style="margin-left: auto" @click="openPreset(p)"><Icon name="schedules" />Schedule</button>
              </div>
            </article>
          </div>
        </section>

        <section class="card" aria-labelledby="sched-title">
          <div class="card-head"><h2 id="sched-title"><Icon name="schedules" />Schedules on this project</h2><RouterLink to="/schedules">All schedules</RouterLink></div>
          <div class="table-wrap">
            <table class="table">
              <thead><tr><th>Name</th><th>Cron</th><th>Enabled</th><th>Next run</th><th class="hide-mobile">Last run</th><th><span class="sr-only">Actions</span></th></tr></thead>
              <tbody>
                <SkeletonRows v-if="schedules === null" :cols="6" :rows="2" />
                <tr v-else-if="!schedules.length"><td colspan="6"><EmptyState title="Nothing scheduled" icon="schedules" compact>Pick a preset above to keep this repository healthy.</EmptyState></td></tr>
                <tr v-for="s in schedules ?? []" :key="s.id">
                  <td class="cell-title">{{ s.name }}</td>
                  <td><code class="label-chip">{{ s.cron }}</code></td>
                  <td><label class="switch"><input type="checkbox" :checked="s.enabled" :disabled="!auth.isOperator" :aria-label="`Enable ${s.name}`" @change="toggleSchedule(s)" /></label></td>
                  <td class="nowrap" :title="fmtDate(s.next_run_at)">{{ s.enabled ? relTime(s.next_run_at, now) : '—' }}</td>
                  <td class="nowrap hide-mobile" :title="fmtDate(s.last_run_at)">{{ relTime(s.last_run_at, now) }}</td>
                  <td class="right nowrap">
                    <div v-if="auth.isOperator" class="row end" style="gap: 4px">
                      <button type="button" class="btn btn-sm" @click="runSchedule(s)"><Icon name="play" />Run now</button>
                      <button type="button" class="btn btn-sm btn-ghost btn-icon btn-danger-ghost" :aria-label="`Delete ${s.name}`" title="Delete" @click="deleteSchedule(s)"><Icon name="trash" /></button>
                    </div>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>
      </div>

      <!-- chat -->
      <div v-else-if="tab === 'chat'" id="ppanel-chat" role="tabpanel" aria-labelledby="ptab-chat" class="stack loose">
        <form v-if="auth.isOperator" class="card" @submit.prevent="startChat">
          <div class="card-head"><h2><Icon name="chat" />Start a coding chat</h2></div>
          <div class="card-body stack">
            <p class="small muted" style="margin: 0">
              Work with an agent interactively on {{ project.owner }}/{{ project.repo }}. The session gets its own branch; ask it to change code, run the tests, commit, push and open a pull request.
            </p>
            <div class="grid-2">
              <div class="field">
                <label for="pc-agent">Agent</label>
                <select id="pc-agent" v-model="chatAgent" class="select">
                  <option value="" disabled>Select an agent</option>
                  <option v-for="a in chatable" :key="a.id" :value="a.id">{{ a.name }} · {{ a.status }}{{ a.id === project.agent_id ? ' (preferred)' : '' }}</option>
                </select>
                <span v-if="!chatable.length" class="hint warn">No enrolled agents yet.</span>
                <span v-else-if="chatAgentObj && chatAgentObj.status !== 'online'" class="hint warn">{{ chatAgentObj.name }} is {{ chatAgentObj.status }}; messages wait until it connects.</span>
              </div>
              <div class="field">
                <label for="pc-title">Title <span class="opt">(optional)</span></label>
                <input id="pc-title" v-model="chatTitle" class="input" maxlength="200" :placeholder="`Coding on ${project.name}`" />
              </div>
            </div>
          </div>
          <div class="card-foot row end">
            <button type="submit" class="btn btn-primary" :disabled="!chatAgent || starting"><span v-if="starting" class="spinner" /><Icon v-else name="chat" />Start chat</button>
          </div>
        </form>

        <section class="card" aria-labelledby="chats-title">
          <div class="card-head"><h2 id="chats-title"><Icon name="sessions" />Chats on this project</h2></div>
          <div class="table-wrap">
            <table class="table">
              <thead><tr><th>Session</th><th>Agent</th><th>State</th><th class="hide-mobile">Branch</th><th class="hide-mobile">Last activity</th></tr></thead>
              <tbody>
                <SkeletonRows v-if="sessions === null" :cols="5" :rows="2" />
                <tr v-else-if="!sessions.length"><td colspan="5"><EmptyState title="No chats yet" icon="chat" compact>Coding chats on this project show up here.</EmptyState></td></tr>
                <tr v-for="s in sessions ?? []" :key="s.id" class="clickable" tabindex="0" @click="router.push(`/sessions/${s.id}`)" @keydown.enter="router.push(`/sessions/${s.id}`)">
                  <td><RouterLink :to="`/sessions/${s.id}`" class="cell-title" @click.stop>{{ s.title || 'Chat' }}</RouterLink><div class="cell-sub">{{ relTime(s.created_at, now) }}</div></td>
                  <td class="nowrap">{{ catalog.agentName(s.agent_id) }}</td>
                  <td><Badge :value="s.status === 'closed' ? 'closed' : s.state || 'idle'" /></td>
                  <td class="mono small hide-mobile">{{ s.branch || '—' }}</td>
                  <td class="nowrap hide-mobile">{{ relTime(s.last_activity_at ?? s.updated_at, now) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>
      </div>
    </div>

    <NewTaskModal :open="showTask" :initial="taskInitial" lock-project :title="`New coding task · ${project.name}`" @close="showTask = false" @created="onTaskCreated" />

    <Modal :open="!!presetFor" :title="presetFor ? `Schedule: ${presetFor.name}` : ''" :dismissable="!scheduling" @close="presetFor = null">
      <form v-if="presetFor" id="preset-form" class="stack" @submit.prevent="schedulePreset">
        <p class="small muted" style="margin: 0">{{ presetFor.description }}</p>
        <div class="field">
          <label for="ps-cron">Cron (UTC)</label>
          <input id="ps-cron" v-model="pCron" class="input mono" required />
          <span class="hint">{{ CRON_HUMAN[pCron.trim()] ?? 'minute hour day-of-month month day-of-week' }}</span>
        </div>
        <div class="field">
          <label for="ps-aut">Autonomy</label>
          <select id="ps-aut" v-model.number="pAutonomy" class="select">
            <option v-for="l in AUTONOMY_LEVELS.filter((x) => x.value > 0)" :key="l.value" :value="l.value">{{ l.label }}</option>
          </select>
          <span class="hint">Anything riskier than the level pauses for a human approval.</span>
        </div>
        <label class="switch"><input v-model="pEnabled" type="checkbox" />Enabled: runs on schedule</label>
      </form>
      <template #footer>
        <button type="button" class="btn" @click="presetFor = null">Cancel</button>
        <button type="submit" form="preset-form" class="btn btn-primary" :disabled="scheduling || !pCron.trim()"><span v-if="scheduling" class="spinner" />Schedule</button>
      </template>
    </Modal>
  </div>
</template>

<style scoped>
.repo-line {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.preset-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(250px, 1fr));
  gap: 14px;
}
.preset {
  padding: 14px 16px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.preset-ic {
  width: 34px;
  height: 34px;
  flex: none;
  border-radius: var(--radius);
  display: grid;
  place-items: center;
  background: var(--primary-50);
  color: var(--primary-text);
}
.preset-ic .icon {
  width: 18px;
  height: 18px;
}
</style>
