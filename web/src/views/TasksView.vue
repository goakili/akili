<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, type Approval, type Question, type Task } from '../api'
import { useAuth } from '../stores/auth'
import { useCatalog } from '../stores/catalog'
import { useLive } from '../stores/live'
import { useCoder } from '../stores/coder'
import { relTime, usd } from '../lib/format'
import { useNow } from '../lib/now'
import { fetchAll, usePaged } from '../lib/paged'
import Badge from '../components/Badge.vue'
import NewTaskModal from '../components/NewTaskModal.vue'
import ProjectChip from '../components/ProjectChip.vue'
import PrLink from '../components/PrLink.vue'
import TriggerChip from '../components/TriggerChip.vue'
import PageHeader from '../components/PageHeader.vue'
import EmptyState from '../components/EmptyState.vue'
import InfiniteScroll from '../components/InfiniteScroll.vue'
import SkeletonRows from '../components/SkeletonRows.vue'
import Icon, { type IconName } from '../components/Icon'

const auth = useAuth()
const catalog = useCatalog()
const live = useLive()
const coder = useCoder()
const route = useRoute()
const router = useRouter()
const now = useNow()

const ACTIVE = 'draft,queued,assigned,running'
const FINISHED = 'succeeded,failed,cancelled,timed_out'
const isActive = (t: Task) => ACTIVE.split(',').includes(t.status)
/** Open work is small by nature and always shown in full; finished tasks load as the page scrolls. */
const active = ref<Task[]>([])
const finished = usePaged((page) => api.pageTasks({ status: FINISHED, page }))
const { loadingMore, hasMore } = finished
const tasks = computed(() => [...active.value, ...finished.items.value])
/** task id → number of pending approvals */
const waiting = ref<Map<string, number>>(new Map())
/** ids of tasks with a question waiting for an answer */
const asking = ref<Set<string>>(new Set())
const loading = ref(true)
const filter = ref('')
const view = computed<'board' | 'list'>(() => (route.query.view === 'list' ? 'list' : 'board'))
const showNew = ref(false)
/** ?project=<id> preselects a project in the new-task dialog. */
const newInitial = computed(() => (typeof route.query.project === 'string' ? { project_id: route.query.project } : null))

function setView(v: 'board' | 'list') {
  router.replace({ query: { ...route.query, view: v === 'list' ? 'list' : undefined } })
}

type ColKey = 'queued' | 'running' | 'approval' | 'done' | 'failed'
const COLUMNS: { key: ColKey; title: string; icon: IconName; tone: string }[] = [
  { key: 'queued', title: 'Queued', icon: 'clock', tone: 'var(--text-tertiary)' },
  { key: 'running', title: 'Running', icon: 'loader', tone: 'var(--info-text)' },
  { key: 'approval', title: 'Needs you', icon: 'approvals', tone: 'var(--warning-text)' },
  { key: 'done', title: 'Done', icon: 'checkCircle', tone: 'var(--success-text)' },
  { key: 'failed', title: 'Failed', icon: 'xCircle', tone: 'var(--danger-text)' },
]

async function loadApprovals() {
  try {
    const a = await fetchAll((page) => api.pageApprovals({ status: 'pending', page, size: 200 }, { quiet: true }))
    const m = new Map<string, number>()
    for (const x of a) if (x.task_id) m.set(x.task_id, (m.get(x.task_id) ?? 0) + 1)
    waiting.value = m
  } catch {
    /* the board still works without the approval column */
  }
}

async function loadQuestions() {
  try {
    const qs = await fetchAll((page) => api.pageQuestions({ status: 'pending', page, size: 200 }, { quiet: true }))
    asking.value = new Set(qs.map((q) => q.task_id).filter((id): id is string => !!id))
  } catch {
    /* the board still works without questions */
  }
}

async function load() {
  try {
    const [a] = await Promise.all([fetchAll((page) => api.pageTasks({ status: ACTIVE, page, size: 200 })), finished.reload(), loadApprovals(), loadQuestions()])
    active.value = a
  } catch {
    /* toasted */
  } finally {
    loading.value = false
  }
}

const filtered = computed(() => {
  const q = filter.value.trim().toLowerCase()
  if (!q) return tasks.value
  return tasks.value.filter(
    (t) =>
      t.title.toLowerCase().includes(q) ||
      t.goal.toLowerCase().includes(q) ||
      catalog.agentName(t.assigned_agent_id ?? t.agent_id).toLowerCase().includes(q) ||
      (t.selector ?? []).some((l) => l.toLowerCase().includes(q)) ||
      (t.branch ?? '').toLowerCase().includes(q) ||
      projectLabel(t).toLowerCase().includes(q),
  )
})

/** The list view keeps the server's newest-first order across open and finished tasks. */
const listed = computed(() => [...filtered.value].sort((a, b) => b.created_at.localeCompare(a.created_at)))
/** Finished columns only hold the pages loaded so far. */
const countLabel = (k: ColKey) => `${byColumn.value[k].length}${hasMore.value && (k === 'done' || k === 'failed') ? '+' : ''}`

function column(t: Task): ColKey {
  if (t.status === 'queued' || t.status === 'draft') return 'queued'
  if (t.status === 'succeeded') return 'done'
  if (t.status === 'failed' || t.status === 'cancelled' || t.status === 'timed_out') return 'failed'
  return waiting.value.has(t.id) || asking.value.has(t.id) ? 'approval' : 'running'
}
function displayStatus(t: Task): string {
  if (column(t) !== 'approval') return t.status
  return waiting.value.has(t.id) ? 'needs_approval' : 'needs_input'
}

const byColumn = computed(() => {
  const out: Record<ColKey, Task[]> = { queued: [], running: [], approval: [], done: [], failed: [] }
  for (const t of filtered.value) out[column(t)].push(t)
  out.queued.sort((a, b) => b.priority - a.priority || a.created_at.localeCompare(b.created_at))
  for (const k of ['running', 'approval', 'done', 'failed'] as const) out[k].sort((a, b) => b.updated_at.localeCompare(a.updated_at))
  return out
})

function target(t: Task) {
  if (t.assigned_agent_id) return catalog.agentName(t.assigned_agent_id)
  if (t.agent_id) return catalog.agentName(t.agent_id)
  if (t.selector?.length) return t.selector.join(', ')
  return 'any agent'
}

function projectLabel(t: Task): string {
  const p = coder.project(t.project_id)
  return p ? `${p.owner}/${p.repo} ${p.name}` : ''
}

function openNew() {
  showNew.value = true
}
function closeNew() {
  showNew.value = false
  if (route.query.new || route.query.project) router.replace({ query: { ...route.query, new: undefined, project: undefined } })
}
function onCreated(t: Task) {
  upsert(t)
  closeNew()
}

function upsert(t: Task) {
  if (isActive(t)) {
    finished.remove(t.id)
    const i = active.value.findIndex((x) => x.id === t.id)
    if (i >= 0) active.value[i] = t
    else active.value.unshift(t)
    return
  }
  active.value = active.value.filter((x) => x.id !== t.id)
  finished.upsert(t)
}

let off: (() => void) | null = null
let offRe: (() => void) | null = null
onMounted(() => {
  load()
  catalog.loadAgents()
  coder.loadProjects()
  coder.loadIntegrations()
  if (route.query.new && auth.isOperator) openNew()
  off = live.on((ev) => {
    if (ev.type === 'task.updated' && ev.data) upsert(ev.data as Task)
    if (ev.type === 'approval.created' || ev.type === 'approval.resolved') {
      const a = ev.data as Approval | undefined
      if (a?.task_id) loadApprovals()
    }
    if (ev.type === 'question.created' || ev.type === 'question.resolved') {
      if ((ev.data as Question | undefined)?.task_id) loadQuestions()
    }
  })
  offRe = live.onReconnect(load)
})
onUnmounted(() => {
  off?.()
  offRe?.()
})

const open = (id: string) => router.push(`/tasks/${id}`)
</script>

<template>
  <div>
    <PageHeader title="Tasks" subtitle="Autonomous work, from queue to result. The board updates live.">
      <button v-if="auth.isOperator" type="button" class="btn btn-primary" @click="openNew"><Icon name="plus" />New task</button>
    </PageHeader>

    <div class="toolbar">
      <div class="segmented" role="group" aria-label="View">
        <button type="button" :class="{ on: view === 'board' }" :aria-pressed="view === 'board'" @click="setView('board')"><Icon name="board" />Board</button>
        <button type="button" :class="{ on: view === 'list' }" :aria-pressed="view === 'list'" @click="setView('list')"><Icon name="list" />List</button>
      </div>
      <div class="search-input" style="margin-left: auto; width: min(300px, 100%)">
        <Icon name="search" />
        <input v-model="filter" class="input" type="search" placeholder="Search tasks, agents, repos" aria-label="Search tasks" />
      </div>
    </div>

    <div v-if="!loading && !tasks.length" class="card">
      <EmptyState title="No tasks yet" icon="tasks">
        A task is a goal an agent works on by itself. Risky steps pause here for your approval.
        <template v-if="auth.isOperator" #actions><button type="button" class="btn btn-primary" @click="openNew"><Icon name="plus" />Create your first task</button></template>
      </EmptyState>
    </div>

    <div v-else-if="view === 'board'" class="board">
      <section v-for="c in COLUMNS" :key="c.key" class="column" :aria-labelledby="`col-${c.key}`">
        <div class="column-head">
          <Icon :name="c.icon" :style="{ color: c.tone }" />
          <span :id="`col-${c.key}`">{{ c.title }}</span>
          <span class="count" :aria-label="`${countLabel(c.key)} tasks`">{{ loading ? '…' : countLabel(c.key) }}</span>
        </div>
        <template v-if="loading">
          <div v-for="i in 2" :key="i" class="task-card" aria-hidden="true"><span class="skel" style="width: 80%" /><span class="skel" style="width: 50%; margin-top: 10px" /></div>
        </template>
        <article v-for="t in byColumn[c.key]" :key="t.id" class="task-card stretch-card">
          <RouterLink :to="`/tasks/${t.id}`" class="tt stretch-link">{{ t.title || t.goal }}</RouterLink>
          <div class="tm">
            <Badge :value="displayStatus(t)" />
            <TriggerChip :task="t" />
            <span v-if="(waiting.get(t.id) ?? 0) > 1" class="xs muted">{{ waiting.get(t.id) }} calls waiting</span>
          </div>
          <div class="tm" style="margin-top: 8px">
            <span><Icon name="agents" />{{ target(t) }}</span>
            <span><Icon name="clock" />{{ relTime(t.updated_at, now) }}</span>
            <span v-if="t.cost_usd"><Icon name="dollar" />{{ usd(t.cost_usd) }}</span>
            <span v-if="t.attempts > 1">attempt {{ t.attempts }}/{{ t.max_attempts }}</span>
          </div>
          <div v-if="t.project_id" class="tm code-row">
            <ProjectChip :project="coder.project(t.project_id)" :branch="t.branch" compact />
            <PrLink v-if="t.pr_url" :url="t.pr_url" :number="t.pr_number" />
          </div>
          <div v-if="t.status_reason && c.key !== 'done'" class="xs muted truncate" style="margin-top: 6px">{{ t.status_reason }}</div>
        </article>
        <div v-if="!loading && !byColumn[c.key].length" class="column-empty">Nothing here</div>
      </section>
    </div>

    <div v-else class="card">
      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr><th>Task</th><th>Status</th><th class="hide-mobile">Target</th><th class="hide-mobile">Autonomy</th><th class="right hide-mobile">Attempts</th><th class="right">Cost</th><th class="hide-mobile">Updated</th></tr>
          </thead>
          <tbody>
            <SkeletonRows v-if="loading" :cols="7" />
            <tr v-else-if="!filtered.length"><td colspan="7"><EmptyState title="No matching tasks" icon="search" compact>Try a different search.</EmptyState></td></tr>
            <tr v-for="t in loading ? [] : listed" :key="t.id" class="clickable" tabindex="0" @click="open(t.id)" @keydown.enter="open(t.id)">
              <td style="max-width: 420px">
                <RouterLink :to="`/tasks/${t.id}`" class="cell-title truncate" style="display: block" @click.stop>{{ t.title || t.goal }}</RouterLink>
                <div v-if="t.project_id" class="row code-row" style="gap: 6px; margin-top: 4px">
                  <ProjectChip :project="coder.project(t.project_id)" :branch="t.branch" />
                  <PrLink v-if="t.pr_url" :url="t.pr_url" :number="t.pr_number" />
                </div>
                <div v-if="t.status_reason" class="cell-sub truncate">{{ t.status_reason }}</div>
              </td>
              <td><span class="row" style="gap: 4px; flex-wrap: wrap"><Badge :value="displayStatus(t)" /><TriggerChip :task="t" /></span></td>
              <td class="nowrap hide-mobile">{{ target(t) }}</td>
              <td class="hide-mobile"><span class="badge outline square">L{{ t.autonomy }}</span></td>
              <td class="right num hide-mobile">{{ t.attempts }}/{{ t.max_attempts }}</td>
              <td class="right num">{{ usd(t.cost_usd) }}</td>
              <td class="nowrap hide-mobile">{{ relTime(t.updated_at, now) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <InfiniteScroll v-if="!loading && tasks.length" :has-more="hasMore" :loading="loadingMore" @more="finished.more" />

    <NewTaskModal :open="showNew" :initial="newInitial" @close="closeNew" @created="onCreated" />
  </div>
</template>
