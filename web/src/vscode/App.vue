<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { api, type Agent, type ChatSession, type Project, type Task } from '../api'
import { useAuth } from '../stores/auth'
import { useLive } from '../stores/live'
import { relTime, shortId } from '../lib/format'
import { taskFormFrom, taskInput } from '../lib/taskForm'
import SessionTranscript from '../components/SessionTranscript.vue'
import Badge from '../components/Badge.vue'
import ToastHost from '../components/ToastHost.vue'
import Icon from '../components/Icon'
import { onHost, vscode } from './bridge'

type TaskFilter = 'active' | 'all' | 'done'
const ACTIVE = new Set(['queued', 'assigned', 'running'])
const FILTERS: { id: TaskFilter; label: string }[] = [
  { id: 'active', label: 'Active' },
  { id: 'all', label: 'All' },
  { id: 'done', label: 'Finished' },
]

const auth = useAuth()
const live = useLive()

const project = ref<Project | null>(null)
const projectKnown = ref(false)
const account = ref({ email: '', server: '' })
const tab = ref<'chat' | 'tasks'>('chat')
const sessions = ref<ChatSession[]>([])
const tasks = ref<Task[]>([])
const agents = ref<Agent[]>([])
const current = ref<string | null>(null)
const loading = ref(false)
const loaded = ref(false)
const loadError = ref('')
const search = ref('')
const taskFilter = ref<TaskFilter>('active')

const newChat = ref(false)
const chatAgent = ref('')
const newTask = ref(false)
const taskGoal = ref('')
const taskTitle = ref('')
const busy = ref(false)
const goalInput = ref<HTMLTextAreaElement | null>(null)

const agentName = (id: string | null) => agents.value.find((a) => a.id === id)?.name ?? (id ? shortId(id) : '')
const onlineAgents = computed(() => agents.value.filter((a) => a.status === 'online'))
const projectAgent = computed(() => agents.value.find((a) => a.id === project.value?.agent_id) ?? null)
const currentSession = computed(() => sessions.value.find((s) => s.id === current.value) ?? null)
const activeTasks = computed(() => tasks.value.filter((t) => ACTIVE.has(t.status)).length)
const openChats = computed(() => sessions.value.filter((s) => s.status === 'open').length)

const q = computed(() => search.value.trim().toLowerCase())
const shownSessions = computed(() =>
  sessions.value.filter((s) => !q.value || `${s.title} ${agentName(s.agent_id)} ${s.id}`.toLowerCase().includes(q.value)),
)
const shownTasks = computed(() =>
  tasks.value.filter((t) => {
    if (taskFilter.value === 'active' && !ACTIVE.has(t.status)) return false
    if (taskFilter.value === 'done' && ACTIVE.has(t.status)) return false
    return !q.value || `${t.title} ${t.goal} ${t.id}`.toLowerCase().includes(q.value)
  }),
)
const initials = computed(() => {
  const e = account.value.email || auth.user?.email || '?'
  return e.slice(0, 2).toUpperCase()
})
const serverHost = computed(() => {
  try {
    return new URL(account.value.server).host
  } catch {
    return account.value.server
  }
})

async function load() {
  if (!project.value) return
  const pid = project.value.id
  loading.value = true
  loadError.value = ''
  try {
    const [s, t, a] = await Promise.all([
      api.listSessions({ project_id: pid, mode: 'chat', limit: 50 }),
      api.listTasks({ project_id: pid, limit: 50 }, { quiet: true }),
      api.listAgents({ quiet: true }),
    ])
    if (project.value?.id !== pid) return
    sessions.value = s ?? []
    tasks.value = t ?? []
    agents.value = a ?? []
    chatAgent.value ||= (projectAgent.value?.status === 'online' ? projectAgent.value.id : onlineAgents.value[0]?.id) ?? ''
    loaded.value = true
  } catch (e) {
    loadError.value = (e as Error).message
  } finally {
    loading.value = false
  }
}

async function startChat() {
  if (!project.value || !chatAgent.value) return
  busy.value = true
  try {
    const s = await api.createSession(chatAgent.value, '', project.value.id)
    sessions.value.unshift(s)
    current.value = s.id
    newChat.value = false
  } finally {
    busy.value = false
  }
}

async function startTask() {
  if (!project.value || !taskGoal.value.trim()) return
  busy.value = true
  try {
    const input = taskInput({ ...taskFormFrom({ project_id: project.value.id }), title: taskTitle.value.trim(), goal: taskGoal.value.trim() })
    const t = await api.createTask({ ...input, project_id: project.value.id })
    tasks.value.unshift(t)
    taskGoal.value = taskTitle.value = ''
    newTask.value = false
    taskFilter.value = 'active'
  } finally {
    busy.value = false
  }
}

function openNewChat() {
  tab.value = 'chat'
  current.value = null
  newChat.value = true
}

async function openNewTask() {
  tab.value = 'tasks'
  newTask.value = true
  await nextTick()
  goalInput.value?.focus()
}

function openTranscript(t: Task) {
  if (!t.session_id) return
  tab.value = 'chat'
  current.value = t.session_id
}

function command(name: string) {
  vscode.postMessage({ type: 'command', command: name })
}

function openWeb(path: string) {
  vscode.postMessage({ type: 'openWeb', path })
}

watch(current, (id) => live.focus(id))
watch(tab, () => (search.value = ''))

let offLive: (() => void) | null = null
let offHost: (() => void) | null = null
let reloadTimer: ReturnType<typeof setTimeout> | null = null
// Several events arrive together (task.updated, session.state…): reload once.
function scheduleLoad() {
  if (reloadTimer) clearTimeout(reloadTimer)
  reloadTimer = setTimeout(() => void load(), 300)
}

onMounted(async () => {
  offHost = onHost((msg) => {
    switch (msg.type) {
      case 'project':
        project.value = (msg.project as Project | null) ?? null
        projectKnown.value = true
        current.value = null
        loaded.value = false
        sessions.value = []
        tasks.value = []
        void load()
        break
      case 'account':
        account.value = { email: String(msg.email ?? ''), server: String(msg.server ?? '') }
        break
      case 'openSession':
        if (typeof msg.id === 'string') {
          tab.value = 'chat'
          current.value = msg.id
          void load()
        }
        break
      case 'newChat':
        openNewChat()
        break
      case 'newTask':
        void openNewTask()
        break
      case 'refresh':
        void load()
        break
    }
  })
  await auth.load()
  live.start(() => vscode.postMessage({ type: 'unauthorized' }))
  offLive = live.on((ev) => {
    if (ev.type.startsWith('task.') || ev.type.startsWith('session.')) scheduleLoad()
  })
  vscode.postMessage({ type: 'ready' })
})
onUnmounted(() => {
  offLive?.()
  offHost?.()
  if (reloadTimer) clearTimeout(reloadTimer)
  live.stop()
})
</script>

<template>
  <div class="vsc">
    <ToastHost />

    <div v-if="!projectKnown" class="vsc-center"><span class="spinner" aria-label="Loading" /></div>

    <section v-else-if="!project" class="vsc-center vsc-empty">
      <Icon name="repo" :size="28" />
      <h2>Link this folder to a project</h2>
      <p class="muted">Chats and tasks in this sidebar belong to an Akili project. Link the folder to the project that matches its repository.</p>
      <button class="btn btn-primary btn-sm" @click="command('linkProject')"><Icon name="link" />Link a project</button>
      <button class="btn btn-sm" @click="openWeb('/projects')"><Icon name="external" />Browse projects</button>
    </section>

    <template v-else>
      <header class="vsc-head">
        <div class="vsc-project">
          <strong :title="`${project.owner}/${project.repo}`">{{ project.name }}</strong>
          <span v-if="projectAgent" class="vsc-agent" :title="`Preferred agent: ${projectAgent.name}`">
            <span class="dot" :class="projectAgent.status" />{{ projectAgent.name }}
          </span>
        </div>
        <nav class="vsc-tabs" role="tablist">
          <button role="tab" :aria-selected="tab === 'chat'" :class="{ on: tab === 'chat' }" @click="tab = 'chat'">
            Chat<span v-if="openChats" class="count">{{ openChats }}</span>
          </button>
          <button role="tab" :aria-selected="tab === 'tasks'" :class="{ on: tab === 'tasks' }" @click="tab = 'tasks'">
            Tasks<span v-if="activeTasks" class="count">{{ activeTasks }}</span>
          </button>
          <span v-if="loading && loaded" class="spinner vsc-mini" aria-label="Refreshing" />
        </nav>
      </header>

      <div v-if="loadError" class="vsc-panel">
        <div class="banner danger" role="alert">
          <Icon name="alert" />
          <div class="banner-body">Could not load this project: {{ loadError }}</div>
        </div>
        <button class="btn btn-sm" @click="load"><Icon name="refresh" />Try again</button>
      </div>

      <div v-else-if="!loaded" class="vsc-center"><span class="spinner" aria-label="Loading" /></div>

      <!-- Chat -->
      <section v-else-if="tab === 'chat' && currentSession" class="vsc-panel vsc-session">
        <div class="vsc-bar">
          <button class="btn btn-ghost btn-sm btn-icon" title="All chats" @click="current = null"><Icon name="chevronLeft" /></button>
          <div class="vsc-bar-title">
            <span class="vsc-title">{{ currentSession.title || 'Chat ' + shortId(currentSession.id) }}</span>
            <span class="small muted">{{ agentName(currentSession.agent_id) }}</span>
          </div>
          <button class="btn btn-ghost btn-sm btn-icon" title="Open in the browser" @click="openWeb(`/sessions/${currentSession.id}`)"><Icon name="external" /></button>
        </div>
        <SessionTranscript :key="currentSession.id" :session-id="currentSession.id" class="vsc-transcript" />
      </section>

      <section v-else-if="tab === 'chat'" class="vsc-panel">
        <div v-if="newChat" class="card vsc-form stack">
          <label for="vsc-agent">Chat with</label>
          <select id="vsc-agent" v-model="chatAgent" class="select" :disabled="!onlineAgents.length">
            <option v-for="a in onlineAgents" :key="a.id" :value="a.id">{{ a.name }}{{ a.id === project.agent_id ? ' (project agent)' : '' }}</option>
          </select>
          <p v-if="!onlineAgents.length" class="hint warn" style="margin: 0">No agent is online. Start one, or check the fleet in the browser.</p>
          <div class="row vsc-actions">
            <button class="btn btn-sm" @click="newChat = false">Cancel</button>
            <button class="btn btn-sm btn-primary" :disabled="busy || !chatAgent" @click="startChat"><span v-if="busy" class="spinner" />Start chat</button>
          </div>
        </div>
        <button v-else class="btn btn-primary btn-sm btn-block" :disabled="!auth.isOperator" :title="auth.isOperator ? '' : 'Operators and admins can start chats'" @click="openNewChat">
          <Icon name="plus" />New chat
        </button>
        <input v-if="sessions.length > 5" v-model="search" class="input vsc-search" type="search" placeholder="Search chats" aria-label="Search chats" />
        <ul class="vsc-list">
          <li v-for="s in shownSessions" :key="s.id">
            <button class="vsc-item" @click="current = s.id">
              <span class="vsc-item-row">
                <span class="vsc-item-title">{{ s.title || 'Chat ' + shortId(s.id) }}</span>
                <span v-if="s.state && s.state !== 'idle' && s.status === 'open'" class="vsc-state">{{ s.state.replace('_', ' ') }}</span>
              </span>
              <span class="small muted">{{ agentName(s.agent_id) }} · {{ s.status === 'closed' ? 'closed · ' : '' }}{{ relTime(s.last_activity_at ?? s.created_at) }}</span>
            </button>
          </li>
        </ul>
        <div v-if="!sessions.length" class="vsc-empty-list">
          <Icon name="chat" :size="22" />
          <p class="muted">No chats on this project yet. Start one to ask an agent about the code.</p>
        </div>
        <p v-else-if="!shownSessions.length" class="small muted">No chat matches "{{ search }}".</p>
      </section>

      <!-- Tasks -->
      <section v-else class="vsc-panel">
        <div v-if="newTask" class="card vsc-form stack">
          <label for="vsc-goal">What should the agent do?</label>
          <textarea
            id="vsc-goal"
            ref="goalInput"
            v-model="taskGoal"
            class="textarea"
            rows="5"
            placeholder="e.g. Add a /version endpoint and a test for it"
            @keydown.meta.enter="startTask"
            @keydown.ctrl.enter="startTask"
          />
          <label for="vsc-title">Title <span class="opt">(optional)</span></label>
          <input id="vsc-title" v-model="taskTitle" class="input" maxlength="200" />
          <p class="hint" style="margin: 0">The agent works on a new <code>akili/…</code> branch and opens a pull request.</p>
          <div class="row vsc-actions">
            <button class="btn btn-sm" @click="newTask = false">Cancel</button>
            <button class="btn btn-sm btn-primary" :disabled="busy || !taskGoal.trim()" @click="startTask"><span v-if="busy" class="spinner" />Create task</button>
          </div>
        </div>
        <button v-else class="btn btn-primary btn-sm btn-block" :disabled="!auth.isOperator" :title="auth.isOperator ? '' : 'Operators and admins can create tasks'" @click="openNewTask">
          <Icon name="plus" />New task
        </button>
        <div class="vsc-filters" role="group" aria-label="Show tasks">
          <button v-for="f in FILTERS" :key="f.id" :class="{ on: taskFilter === f.id }" :aria-pressed="taskFilter === f.id" @click="taskFilter = f.id">{{ f.label }}</button>
        </div>
        <input v-if="tasks.length > 5" v-model="search" class="input vsc-search" type="search" placeholder="Search tasks" aria-label="Search tasks" />
        <ul class="vsc-list">
          <li v-for="t in shownTasks" :key="t.id" class="vsc-task">
            <button class="vsc-task-head" :disabled="!t.session_id" :title="t.session_id ? 'Open the transcript' : ''" @click="openTranscript(t)">
              <span class="vsc-item-title">{{ t.title || t.goal.slice(0, 90) }}</span>
              <Badge :value="t.status" />
            </button>
            <span class="small muted">{{ shortId(t.id) }} · {{ relTime(t.created_at) }}<template v-if="t.branch"> · <code>{{ t.branch }}</code></template></span>
            <div class="vsc-task-actions">
              <button v-if="t.branch" class="btn btn-ghost btn-sm" @click="vscode.postMessage({ type: 'openDiff', taskId: t.id })"><Icon name="gitDiff" />Diff</button>
              <button v-if="t.branch" class="btn btn-ghost btn-sm" @click="vscode.postMessage({ type: 'checkout', taskId: t.id, branch: t.branch })"><Icon name="gitBranch" />Check out</button>
              <button v-if="t.pr_url" class="btn btn-ghost btn-sm" @click="vscode.postMessage({ type: 'openUrl', url: t.pr_url })"><Icon name="gitPR" />PR</button>
            </div>
          </li>
        </ul>
        <div v-if="!tasks.length" class="vsc-empty-list">
          <Icon name="tasks" :size="22" />
          <p class="muted">No tasks on this project yet. Describe a change and an agent will make it on a branch.</p>
        </div>
        <p v-else-if="!shownTasks.length" class="small muted">{{ q ? `No task matches "${search}".` : taskFilter === 'active' ? 'Nothing is running.' : 'No finished tasks.' }}</p>
      </section>
    </template>

    <footer class="vsc-foot">
      <span class="vsc-avatar" aria-hidden="true">{{ initials }}</span>
      <span class="vsc-who">
        <span class="vsc-item-title">{{ account.email || auth.user?.email }}</span>
        <span class="small muted vsc-item-title">{{ serverHost }}</span>
      </span>
      <button class="btn btn-ghost btn-sm btn-icon" title="Settings" @click="command('openSettings')"><Icon name="settings" /></button>
      <button class="btn btn-ghost btn-sm btn-icon" title="Sign out" @click="command('signOut')"><Icon name="logout" /></button>
    </footer>
  </div>
</template>
