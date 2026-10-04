<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
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

const auth = useAuth()
const live = useLive()

const project = ref<Project | null>(null)
const tab = ref<'chat' | 'tasks'>('chat')
const sessions = ref<ChatSession[]>([])
const tasks = ref<Task[]>([])
const agents = ref<Agent[]>([])
const current = ref<string | null>(null)
const newChat = ref(false)
const chatAgent = ref('')
const newTask = ref(false)
const taskGoal = ref('')
const taskTitle = ref('')
const busy = ref(false)

const onlineAgents = computed(() => agents.value.filter((a) => a.status === 'online'))
const currentSession = computed(() => sessions.value.find((s) => s.id === current.value) ?? null)

async function load() {
  if (!project.value) return
  const pid = project.value.id
  const [s, t, a] = await Promise.all([
    api.listSessions({ project_id: pid, mode: 'chat', limit: 30 }),
    api.listTasks({ project_id: pid, limit: 30 }),
    api.listAgents({ quiet: true }),
  ])
  sessions.value = s ?? []
  tasks.value = t ?? []
  agents.value = a ?? []
  chatAgent.value ||= project.value.agent_id ?? onlineAgents.value[0]?.id ?? ''
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
  } finally {
    busy.value = false
  }
}

function openWeb(path: string) {
  vscode.postMessage({ type: 'openWeb', path })
}

watch(current, (id) => live.focus(id))

let offLive: (() => void) | null = null
let offHost: (() => void) | null = null
onMounted(async () => {
  offHost = onHost((msg) => {
    if (msg.type === 'project') {
      project.value = (msg.project as Project | null) ?? null
      current.value = null
      void load()
    } else if (msg.type === 'openSession' && typeof msg.id === 'string') {
      tab.value = 'chat'
      current.value = msg.id
      void load()
    }
  })
  await auth.load()
  live.start(() => vscode.postMessage({ type: 'unauthorized' }))
  offLive = live.on((ev) => {
    if (ev.type.startsWith('task.') || ev.type === 'session.created' || ev.type === 'session.closed') void load()
  })
  vscode.postMessage({ type: 'ready' })
})
onUnmounted(() => {
  offLive?.()
  offHost?.()
  live.stop()
})
</script>

<template>
  <div class="vsc">
    <ToastHost />
    <p v-if="!project" class="vsc-empty muted">Link this folder to an Akili project to chat and run tasks here.</p>
    <template v-else>
      <nav class="vsc-tabs" role="tablist">
        <button role="tab" :aria-selected="tab === 'chat'" :class="{ on: tab === 'chat' }" @click="tab = 'chat'">Chat</button>
        <button role="tab" :aria-selected="tab === 'tasks'" :class="{ on: tab === 'tasks' }" @click="tab = 'tasks'">Tasks</button>
        <span class="vsc-project" :title="project.name">{{ project.name }}</span>
      </nav>

      <section v-if="tab === 'chat'" class="vsc-panel">
        <template v-if="currentSession">
          <div class="vsc-bar">
            <button class="btn btn-ghost btn-sm" @click="current = null"><Icon name="chevronLeft" />Chats</button>
            <span class="vsc-title">{{ currentSession.title || shortId(currentSession.id) }}</span>
            <button class="btn btn-ghost btn-sm btn-icon" title="Open in browser" @click="openWeb(`/sessions/${currentSession.id}`)"><Icon name="external" /></button>
          </div>
          <SessionTranscript :key="currentSession.id" :session-id="currentSession.id" class="vsc-transcript" />
        </template>
        <template v-else>
          <div v-if="newChat" class="card vsc-form stack">
            <label for="vsc-agent">Agent</label>
            <select id="vsc-agent" v-model="chatAgent" class="select">
              <option v-for="a in onlineAgents" :key="a.id" :value="a.id">{{ a.name }}</option>
            </select>
            <p v-if="!onlineAgents.length" class="hint warn">No agent is online.</p>
            <div class="row" style="justify-content: flex-end; gap: 6px">
              <button class="btn btn-sm" @click="newChat = false">Cancel</button>
              <button class="btn btn-sm btn-primary" :disabled="busy || !chatAgent" @click="startChat">Start chat</button>
            </div>
          </div>
          <button v-else class="btn btn-primary btn-sm btn-block" :disabled="!auth.isOperator" @click="newChat = true"><Icon name="plus" />New chat</button>
          <ul class="vsc-list">
            <li v-for="s in sessions" :key="s.id">
              <button class="vsc-item" @click="current = s.id">
                <span class="vsc-item-title">{{ s.title || shortId(s.id) }}</span>
                <span class="small muted">{{ s.status === 'closed' ? 'closed' : s.state || 'idle' }} · {{ relTime(s.last_activity_at ?? s.created_at) }}</span>
              </button>
            </li>
            <li v-if="!sessions.length" class="small muted">No chats on this project yet.</li>
          </ul>
        </template>
      </section>

      <section v-else class="vsc-panel">
        <div v-if="newTask" class="card vsc-form stack">
          <label for="vsc-title">Title <span class="opt">(optional)</span></label>
          <input id="vsc-title" v-model="taskTitle" class="input" maxlength="200" />
          <label for="vsc-goal">Goal</label>
          <textarea id="vsc-goal" v-model="taskGoal" class="textarea" rows="5" placeholder="What should the agent do?" />
          <div class="row" style="justify-content: flex-end; gap: 6px">
            <button class="btn btn-sm" @click="newTask = false">Cancel</button>
            <button class="btn btn-sm btn-primary" :disabled="busy || !taskGoal.trim()" @click="startTask">Create task</button>
          </div>
        </div>
        <button v-else class="btn btn-primary btn-sm btn-block" :disabled="!auth.isOperator" @click="newTask = true"><Icon name="plus" />New task</button>
        <ul class="vsc-list">
          <li v-for="t in tasks" :key="t.id" class="vsc-task">
            <div class="vsc-task-head">
              <span class="vsc-item-title">{{ t.title || t.goal.slice(0, 80) }}</span>
              <Badge :value="t.status" />
            </div>
            <span class="small muted">{{ shortId(t.id) }} · {{ relTime(t.created_at) }}</span>
            <div class="vsc-task-actions">
              <button v-if="t.session_id" class="btn btn-ghost btn-sm" @click="tab = 'chat'; current = t.session_id">Transcript</button>
              <button v-if="t.branch" class="btn btn-ghost btn-sm" @click="vscode.postMessage({ type: 'openDiff', taskId: t.id })">Diff</button>
              <button v-if="t.branch" class="btn btn-ghost btn-sm" @click="vscode.postMessage({ type: 'checkout', taskId: t.id, branch: t.branch })">Check out</button>
              <button v-if="t.pr_url" class="btn btn-ghost btn-sm" @click="vscode.postMessage({ type: 'openUrl', url: t.pr_url })">PR</button>
            </div>
          </li>
          <li v-if="!tasks.length" class="small muted">No tasks on this project yet.</li>
        </ul>
      </section>
    </template>
  </div>
</template>
