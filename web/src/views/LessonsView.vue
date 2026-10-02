<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { api, type Lesson, type LessonStatus } from '../api'
import { useAuth } from '../stores/auth'
import { useCatalog } from '../stores/catalog'
import { useConfirm } from '../stores/confirm'
import { useToast } from '../stores/toast'
import { fmtDate, relTime, shortId } from '../lib/format'
import { useNow } from '../lib/now'
import Badge from '../components/Badge.vue'
import Modal from '../components/Modal.vue'
import PageHeader from '../components/PageHeader.vue'
import EmptyState from '../components/EmptyState.vue'
import SkeletonRows from '../components/SkeletonRows.vue'
import Icon from '../components/Icon'

const auth = useAuth()
const catalog = useCatalog()
const confirm = useConfirm()
const toast = useToast()
const now = useNow()

const MIN_LEN = 10
const MAX_LEN = 400

const status = ref<LessonStatus>('proposed')
const agentFilter = ref('')
const items = ref<Lesson[]>([])
const loading = ref(true)
const proposedCount = ref(0)
const busy = ref<string | null>(null)

const FILTERS: { value: LessonStatus; label: string }[] = [
  { value: 'proposed', label: 'Proposed' },
  { value: 'approved', label: 'Approved' },
  { value: 'rejected', label: 'Rejected' },
]

const agents = computed(() => catalog.agents.filter((a) => a.status !== 'revoked'))

async function load() {
  loading.value = true
  const agent_id = agentFilter.value || undefined
  try {
    items.value = (await api.listLessons({ status: status.value, agent_id })) ?? []
    if (status.value === 'proposed') proposedCount.value = items.value.length
    else proposedCount.value = ((await api.listLessons({ status: 'proposed', agent_id }, { quiet: true }).catch(() => null)) ?? []).length
  } catch {
    /* toasted */
  } finally {
    loading.value = false
  }
}

function setStatus(s: LessonStatus) {
  status.value = s
  items.value = []
  load()
}

watch(agentFilter, () => {
  items.value = []
  load()
})

function scope(l: Lesson): string {
  return l.agent_id ? catalog.agentName(l.agent_id) : 'All agents'
}

function proposer(l: Lesson): string {
  return catalog.agents.find((a) => a.id === l.proposed_by)?.name ?? 'an operator'
}

/** The length the server counts: whitespace collapses because a lesson is one line in the prompt. */
const lessonLen = (t: string) => [...t.replace(/\s+/g, ' ').trim()].length


const deciding = ref<{ lesson: Lesson; action: 'approve' | 'reject' } | null>(null)
const decideText = ref('')
const decideNote = ref('')
const saving = ref(false)
const decideLen = computed(() => lessonLen(decideText.value))
const decideValid = computed(() => deciding.value?.action === 'reject' || (decideLen.value >= MIN_LEN && decideLen.value <= MAX_LEN))

function openDecide(l: Lesson, action: 'approve' | 'reject') {
  deciding.value = { lesson: l, action }
  decideText.value = l.text
  decideNote.value = ''
}

async function decide() {
  const d = deciding.value
  if (!d || !decideValid.value) return
  saving.value = true
  try {
    if (d.action === 'approve') {
      await api.approveLesson(d.lesson.id, { text: decideText.value.trim(), note: decideNote.value.trim() })
      toast.success('Lesson approved: it is added to future sessions')
    } else {
      await api.rejectLesson(d.lesson.id, decideNote.value.trim())
      toast.success('Lesson rejected')
    }
    deciding.value = null
    items.value = items.value.filter((x) => x.id !== d.lesson.id)
    if (status.value === 'proposed') proposedCount.value = items.value.length
  } catch {
    /* toasted */
  } finally {
    saving.value = false
  }
}

async function remove(l: Lesson) {
  const ok = await confirm.ask({
    title: 'Delete this lesson?',
    message: `New sessions of ${l.agent_id ? scope(l) : 'every agent'} no longer get it in their system prompt. Running sessions keep the prompt they started with.`,
    confirmText: 'Delete lesson',
    danger: true,
  })
  if (!ok) return
  busy.value = l.id
  try {
    await api.deleteLesson(l.id)
    items.value = items.value.filter((x) => x.id !== l.id)
    toast.success('Lesson deleted')
  } catch {
    /* toasted */
  } finally {
    busy.value = null
  }
}


const adding = ref(false)
const addText = ref('')
const addAgent = ref('')
const addTried = ref(false)
const addLen = computed(() => lessonLen(addText.value))
const addValid = computed(() => addLen.value >= MIN_LEN && addLen.value <= MAX_LEN)

function openAdd() {
  addText.value = ''
  addAgent.value = agentFilter.value
  addTried.value = false
  adding.value = true
}

async function add() {
  addTried.value = true
  if (!addValid.value) return
  saving.value = true
  try {
    await api.createLesson(addAgent.value ? { text: addText.value.trim(), agent_id: addAgent.value } : { text: addText.value.trim() })
    adding.value = false
    toast.success('Lesson added')
    if (status.value === 'approved') load()
    else setStatus('approved')
  } catch {
    /* toasted */
  } finally {
    saving.value = false
  }
}

onMounted(() => {
  load()
  catalog.loadAgents()
})
</script>

<template>
  <div>
    <PageHeader title="Lessons" subtitle="Short facts agents learn while they work. Approved lessons are added to the system prompt of the agent's future sessions; proposed and rejected ones never are.">
      <button v-if="auth.isAdmin" type="button" class="btn btn-primary" @click="openAdd"><Icon name="plus" />Add lesson</button>
    </PageHeader>

    <div class="banner warn" style="margin-bottom: 16px">
      <Icon name="alert" />
      <div class="banner-body">
        <strong>Read each lesson before approving it.</strong>
        <p>Agents write the text themselves, and it may be shaped by what they read: repositories, issues, web pages or tool output. Open the source session to see the context. An approved lesson becomes an instruction in every future session.</p>
      </div>
    </div>

    <div class="toolbar">
      <div class="segmented" role="group" aria-label="Filter by status">
        <button v-for="f in FILTERS" :key="f.value" type="button" :class="{ on: status === f.value }" :aria-pressed="status === f.value" @click="setStatus(f.value)">
          {{ f.label }}<span v-if="f.value === 'proposed' && proposedCount" class="count">{{ proposedCount }}</span>
        </button>
      </div>
      <label for="ls-agent" class="sr-only">Filter by agent</label>
      <select id="ls-agent" v-model="agentFilter" class="select" style="margin-left: auto; width: auto; min-width: 180px">
        <option value="">All agents</option>
        <option v-for="a in agents" :key="a.id" :value="a.id">{{ a.name }}</option>
      </select>
    </div>

    <div class="card">
      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr>
              <th>Lesson</th><th>Applies to</th><th class="hide-mobile">Source</th>
              <th class="hide-mobile">{{ status === 'proposed' ? 'Proposed' : 'Decided' }}</th>
              <th v-if="auth.isAdmin && status !== 'rejected'"><span class="sr-only">Actions</span></th>
            </tr>
          </thead>
          <tbody>
            <SkeletonRows v-if="loading && !items.length" :cols="auth.isAdmin && status !== 'rejected' ? 5 : 4" :rows="3" />
            <tr v-else-if="!items.length">
              <td :colspan="auth.isAdmin && status !== 'rejected' ? 5 : 4">
                <EmptyState v-if="status === 'proposed'" title="Nothing to review" icon="lightbulb">
                  Agents propose lessons with the <code>lesson_propose</code> tool when they learn something worth remembering. New proposals wait here for a decision.
                </EmptyState>
                <EmptyState v-else-if="status === 'approved'" title="No approved lessons" icon="lightbulb">
                  Approve a proposed lesson, or add one yourself, to give agents knowledge that carries over between sessions.
                  <template v-if="auth.isAdmin" #actions><button type="button" class="btn btn-primary" @click="openAdd"><Icon name="plus" />Add lesson</button></template>
                </EmptyState>
                <EmptyState v-else title="No rejected lessons" icon="lightbulb" compact>Rejected proposals are kept here for the record.</EmptyState>
              </td>
            </tr>
            <tr v-for="l in items" :key="l.id">
              <td style="min-width: 260px">
                <div class="lesson-text">{{ l.text }}</div>
                <div v-if="l.note" class="cell-sub" style="white-space: normal"><Icon name="info" :size="12" style="vertical-align: -1px" /> {{ l.note }}</div>
              </td>
              <td class="nowrap">
                <span v-if="l.agent_id" class="row" style="gap: 6px"><Icon name="agents" :size="15" />{{ scope(l) }}</span>
                <span v-else class="badge accent"><Icon name="globe" />All agents</span>
              </td>
              <td class="hide-mobile nowrap">
                <RouterLink v-if="l.session_id" :to="`/sessions/${l.session_id}`" class="small"><Icon name="chat" :size="13" style="vertical-align: -2px" /> session {{ shortId(l.session_id) }}</RouterLink>
                <div v-if="l.task_id"><RouterLink :to="`/tasks/${l.task_id}`" class="small"><Icon name="tasks" :size="13" style="vertical-align: -2px" /> task {{ shortId(l.task_id) }}</RouterLink></div>
                <span v-if="!l.session_id && !l.task_id" class="small muted">added by hand</span>
                <div class="cell-sub">by {{ proposer(l) }}</div>
              </td>
              <td class="hide-mobile nowrap">
                <template v-if="status === 'proposed'"><span :title="fmtDate(l.created_at)">{{ relTime(l.created_at, now) }}</span></template>
                <template v-else>
                  <Badge :value="l.status" />
                  <div class="cell-sub" :title="fmtDate(l.decided_at)">{{ relTime(l.decided_at ?? l.created_at, now) }}</div>
                </template>
              </td>
              <td v-if="auth.isAdmin && status !== 'rejected'" class="right nowrap">
                <div v-if="l.status === 'proposed'" class="row end" style="gap: 4px">
                  <button type="button" class="btn btn-sm btn-primary" @click="openDecide(l, 'approve')"><Icon name="check" />Approve</button>
                  <button type="button" class="btn btn-sm" @click="openDecide(l, 'reject')"><Icon name="x" />Reject</button>
                </div>
                <button
                  v-else-if="l.status === 'approved'"
                  type="button"
                  class="btn btn-sm btn-ghost btn-icon btn-danger-ghost"
                  aria-label="Delete lesson"
                  title="Delete"
                  :disabled="busy === l.id"
                  @click="remove(l)"
                >
                  <Icon name="trash" />
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <Modal :open="!!deciding" :title="deciding?.action === 'approve' ? 'Approve lesson' : 'Reject lesson'" wide :dismissable="!saving" @close="deciding = null">
      <form v-if="deciding" id="lesson-decide" class="stack" novalidate @submit.prevent="decide">
        <div class="small muted">
          Proposed by {{ proposer(deciding.lesson) }} for <strong>{{ scope(deciding.lesson) }}</strong> {{ relTime(deciding.lesson.created_at, now) }}.
          <RouterLink v-if="deciding.lesson.session_id" :to="`/sessions/${deciding.lesson.session_id}`" target="_blank">Open the source session<Icon name="external" :size="12" style="vertical-align: -1px; margin-left: 3px" /></RouterLink>
        </div>
        <template v-if="deciding.action === 'approve'">
          <div class="field">
            <label for="ld-text">Lesson</label>
            <textarea id="ld-text" v-model="decideText" class="textarea" :class="{ invalid: !decideValid }" rows="4" :maxlength="MAX_LEN + 50" />
            <div class="row between">
              <span v-if="decideLen < MIN_LEN" class="error-msg"><Icon name="alert" />At least {{ MIN_LEN }} characters.</span>
              <span v-else-if="decideLen > MAX_LEN" class="error-msg"><Icon name="alert" />At most {{ MAX_LEN }} characters.</span>
              <span v-else class="hint">Edit it into a clear, general fact. It is added word for word to the system prompt.</span>
              <span class="xs num" :class="decideLen > MAX_LEN ? 'danger-text' : 'muted'">{{ decideLen }}/{{ MAX_LEN }}</span>
            </div>
          </div>
        </template>
        <blockquote v-else class="lesson-quote">{{ deciding.lesson.text }}</blockquote>
        <div class="field">
          <label for="ld-note">Note <span class="opt">(optional)</span></label>
          <input id="ld-note" v-model="decideNote" class="input" maxlength="500" :placeholder="deciding.action === 'approve' ? 'Why this is worth keeping' : 'Why it was rejected'" />
          <span class="hint">For other reviewers; never shown to the agent.</span>
        </div>
      </form>
      <template #footer>
        <button type="button" class="btn" @click="deciding = null">Cancel</button>
        <button type="submit" form="lesson-decide" class="btn" :class="deciding?.action === 'approve' ? 'btn-primary' : 'btn-danger'" :disabled="saving || !decideValid">
          <span v-if="saving" class="spinner" />{{ deciding?.action === 'approve' ? 'Approve lesson' : 'Reject lesson' }}
        </button>
      </template>
    </Modal>

    <Modal :open="adding" title="Add lesson" wide :dismissable="!saving" @close="adding = false">
      <form id="lesson-add" class="stack" novalidate @submit.prevent="add">
        <div class="field">
          <label for="la-text">Lesson <span class="req" aria-hidden="true">*</span></label>
          <textarea
            id="la-text"
            v-model="addText"
            class="textarea"
            :class="{ invalid: addTried && !addValid }"
            rows="4"
            :maxlength="MAX_LEN + 50"
            placeholder="e.g. The staging database is on db-2; never run migrations against db-1."
          />
          <div class="row between">
            <span v-if="addTried && addLen < MIN_LEN" class="error-msg"><Icon name="alert" />At least {{ MIN_LEN }} characters.</span>
            <span v-else-if="addLen > MAX_LEN" class="error-msg"><Icon name="alert" />At most {{ MAX_LEN }} characters.</span>
            <span v-else class="hint">One fact, added word for word to the system prompt. No secrets.</span>
            <span class="xs num" :class="addLen > MAX_LEN ? 'danger-text' : 'muted'">{{ addLen }}/{{ MAX_LEN }}</span>
          </div>
        </div>
        <div class="field">
          <label for="la-agent">Applies to</label>
          <select id="la-agent" v-model="addAgent" class="select">
            <option value="">All agents</option>
            <option v-for="a in agents" :key="a.id" :value="a.id">{{ a.name }}</option>
          </select>
          <span class="hint">The lesson is approved right away and used by new sessions.</span>
        </div>
      </form>
      <template #footer>
        <button type="button" class="btn" @click="adding = false">Cancel</button>
        <button type="submit" form="lesson-add" class="btn btn-primary" :disabled="saving">
          <span v-if="saving" class="spinner" />Add lesson
        </button>
      </template>
    </Modal>
  </div>
</template>

<style scoped>
.lesson-text {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  line-height: 1.5;
}
.lesson-quote {
  margin: 0;
  padding: 10px 14px;
  border-left: 3px solid var(--border-primary);
  background: var(--bg-secondary);
  border-radius: var(--radius);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
</style>
