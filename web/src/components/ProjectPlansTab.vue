<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
// The project page's Plans tab: plans as people write them (description + phases), their progress,
// and the tasks that work on them. Agents only report progress on phases (plan_phase_update).
import { computed, ref, watch } from 'vue'
import { api, type PlanDetail, type PlanPhase, type PlanPhaseInput, type PlanSummary, type PhaseStatus } from '../api'
import { useAuth } from '../stores/auth'
import { useConfirm } from '../stores/confirm'
import { useToast } from '../stores/toast'
import { relTime, shortId } from '../lib/format'
import Badge from './Badge.vue'
import EmptyState from './EmptyState.vue'
import Modal from './Modal.vue'
import PlanPhasesEditor from './PlanPhasesEditor.vue'
import SafeMarkdown from './SafeMarkdown'
import Icon from './Icon'

const props = defineProps<{ projectId: string; plans: PlanSummary[] | null }>()
export interface PhaseFocus {
  phaseId: string
  phaseTitle: string
  planId: string
  planTitle: string
}
const emit = defineEmits<{ (e: 'changed'): void; (e: 'createTask', planIds: string[], goal: string, focus: PhaseFocus | null): void }>()
const auth = useAuth()
const confirm = useConfirm()
const toast = useToast()

type Filter = 'open' | 'done' | 'archived'
const filter = ref<Filter>('open')
const FILTERS: { id: Filter; label: string }[] = [
  { id: 'open', label: 'Open' },
  { id: 'done', label: 'Done' },
  { id: 'archived', label: 'Archived' },
]
const shown = computed(() =>
  (props.plans ?? []).filter((p) => (filter.value === 'open' ? p.status !== 'done' && p.status !== 'archived' : p.status === filter.value)),
)
// The list shows the description's first line as plain text.
const excerpt = (md: string) =>
  (md.split('\n').find((l) => l.trim()) ?? '')
    .replace(/[*_`#>]+/g, '')
    .replace(/\[([^\]]*)\]\([^)]*\)/g, '$1')
    .trim()
const closedOf = (c: PlanSummary['counts']) => (c.done ?? 0) + (c.skipped ?? 0)
const pct = (done: number, total: number) => (total ? Math.round((done / total) * 100) : 0)

// ---- detail ----------------------------------------------------------------------------------------
const openId = ref<string | null>(null)
const detail = ref<PlanDetail | null>(null)
const detailPhases = computed(() => detail.value?.phases ?? [])
const detailClosed = computed(() => detailPhases.value.filter((s) => s.status === 'done' || s.status === 'skipped').length)
const linkable = computed(() => detail.value?.plan.status === 'active' || detail.value?.plan.status === 'in_progress')

function createTask() {
  if (detail.value) emit('createTask', [detail.value.plan.id], `Work on the open phases of the plan "${detail.value.plan.title}".`, null)
}

// One task per phase: the agent gets the whole plan as context and finishes this phase only.
function createPhaseTask(ph: PlanPhase) {
  const p = detail.value?.plan
  if (!p) return
  const goal = ph.done_when ? `Finish the phase "${ph.title}" of the plan "${p.title}". It is done when: ${ph.done_when}` : `Finish the phase "${ph.title}" of the plan "${p.title}".`
  emit('createTask', [p.id], goal, { phaseId: ph.id, phaseTitle: ph.title, planId: p.id, planTitle: p.title })
}

const phaseTasks = (phaseId: string) => {
  const ids = new Set((detail.value?.links ?? []).filter((l) => l.phase_id === phaseId).map((l) => l.task_id))
  return (detail.value?.tasks ?? []).filter((t) => ids.has(t.id))
}
const focusOf = (taskId: string) => {
  const id = detail.value?.links?.find((l) => l.task_id === taskId)?.phase_id
  return id ? detailPhases.value.find((p) => p.id === id)?.title ?? '' : ''
}

async function openPlan(id: string) {
  openId.value = id
  detail.value = null
  editingPhases.value = false
  try {
    detail.value = await api.getPlan(id)
  } catch {
    openId.value = null
  }
}

async function reloadDetail() {
  if (!openId.value) return
  try {
    detail.value = await api.getPlan(openId.value, { quiet: true })
  } catch {
    openId.value = null
    detail.value = null
  }
}

// The list reloads on plan.updated (live); keep the open plan in sync with it.
watch(
  () => props.plans,
  () => {
    if (openId.value && !editingPhases.value) void reloadDetail()
  },
)

const PHASE_STATUSES: { value: PhaseStatus; label: string }[] = [
  { value: 'todo', label: 'To do' },
  { value: 'in_progress', label: 'In progress' },
  { value: 'done', label: 'Done' },
  { value: 'skipped', label: 'Skipped' },
]

// Skipping asks for a reason inline, under the phase.
const skipping = ref<string | null>(null)
const skipNote = ref('')

async function setPhase(st: PlanPhase, status: PhaseStatus, note = st.note) {
  if (!detail.value) return
  if (status === 'skipped' && skipping.value !== st.id) {
    skipping.value = st.id
    skipNote.value = st.note
    return
  }
  skipping.value = null
  if (st.status === status && note === st.note) return
  try {
    const updated = await api.updatePlanPhase(detail.value.plan.id, st.id, { status, note })
    Object.assign(st, updated)
    emit('changed')
  } catch {
    /* toasted */
  }
}

const byAgent = (st: PlanPhase) => st.updated_by.startsWith('agent:')

// ---- create / edit ----------------------------------------------------------------------------------
const formOpen = ref(false)
const formMode = ref<'create' | 'edit'>('create')
const fTitle = ref('')
const fDescription = ref('')
const fPhases = ref<PlanPhaseInput[]>([])
const fDraft = ref(false)
const saving = ref(false)
const tried = ref(false)

function openCreate() {
  formMode.value = 'create'
  fTitle.value = fDescription.value = ''
  fPhases.value = [{ title: '', detail: '' }]
  fDraft.value = false
  tried.value = false
  formOpen.value = true
}

function openEdit() {
  if (!detail.value) return
  formMode.value = 'edit'
  fTitle.value = detail.value.plan.title
  fDescription.value = detail.value.plan.description
  tried.value = false
  formOpen.value = true
}

const cleanPhases = (s: PlanPhaseInput[]) => s.map((x) => ({ ...x, title: x.title.trim(), detail: (x.detail ?? '').trim() })).filter((x) => x.title)

async function save() {
  tried.value = true
  if (!fTitle.value.trim()) return
  saving.value = true
  try {
    if (formMode.value === 'create') {
      const d = await api.createPlan(props.projectId, {
        title: fTitle.value.trim(),
        description: fDescription.value,
        status: fDraft.value ? 'draft' : 'active',
        phases: cleanPhases(fPhases.value),
      })
      toast.success(`Plan created: ${d.plan.title}`)
      formOpen.value = false
      emit('changed')
      await openPlan(d.plan.id)
    } else if (detail.value) {
      await api.updatePlan(detail.value.plan.id, { title: fTitle.value.trim(), description: fDescription.value })
      formOpen.value = false
      emit('changed')
      await reloadDetail()
    }
  } catch {
    /* toasted */
  } finally {
    saving.value = false
  }
}

// ---- phases edited in place ----------------------------------------------------------------------------
const editingPhases = ref(false)
const ePhases = ref<PlanPhaseInput[]>([])
function editPhases() {
  ePhases.value = detailPhases.value.map((s) => ({ id: s.id, title: s.title, detail: s.detail, done_when: s.done_when }))
  editingPhases.value = true
}
async function savePhases() {
  if (!detail.value) return
  saving.value = true
  try {
    detail.value = await api.replacePlanPhases(detail.value.plan.id, cleanPhases(ePhases.value))
    editingPhases.value = false
    emit('changed')
  } catch {
    /* toasted */
  } finally {
    saving.value = false
  }
}

async function setStatus(status: 'draft' | 'active' | 'archived') {
  if (!detail.value) return
  try {
    detail.value.plan = await api.updatePlan(detail.value.plan.id, { status })
    emit('changed')
  } catch {
    /* toasted */
  }
}

async function remove() {
  if (!detail.value) return
  const p = detail.value.plan
  const ok = await confirm.ask({
    title: `Delete plan ${p.title}?`,
    message: 'Its phases are deleted too. Tasks that worked on it keep their history.',
    confirmText: 'Delete plan',
    danger: true,
  })
  if (!ok) return
  try {
    await api.deletePlan(p.id)
    toast.success('Plan deleted')
    openId.value = null
    detail.value = null
    emit('changed')
  } catch {
    /* toasted */
  }
}
</script>

<template>
  <!-- one plan -->
  <section v-if="openId" class="card plan-detail">
    <div class="card-head">
      <button type="button" class="btn btn-ghost btn-sm" @click="openId = null"><Icon name="chevronLeft" />All plans</button>
      <div v-if="detail && auth.isOperator" class="row" style="gap: 6px; flex-wrap: wrap">
        <button v-if="linkable" type="button" class="btn btn-sm btn-primary" @click="createTask"><Icon name="play" />Create task</button>
        <button v-if="detail.plan.status === 'draft'" type="button" class="btn btn-sm btn-primary" @click="setStatus('active')"><Icon name="check" />Activate</button>
        <button type="button" class="btn btn-sm" @click="openEdit"><Icon name="edit" />Edit</button>
        <button v-if="detail.plan.status === 'done'" type="button" class="btn btn-sm" title="Reopen: the phases decide its status again" @click="setStatus('active')"><Icon name="undo" />Reopen</button>
        <button v-if="detail.plan.status !== 'archived'" type="button" class="btn btn-sm" @click="setStatus('archived')"><Icon name="box" />Archive</button>
        <button v-else type="button" class="btn btn-sm" @click="setStatus('active')"><Icon name="undo" />Unarchive</button>
        <button type="button" class="btn btn-sm btn-ghost" aria-label="Delete plan" title="Delete plan" @click="remove"><Icon name="trash" /></button>
      </div>
    </div>
    <div v-if="!detail" class="card-body"><span class="spinner" aria-label="Loading" /></div>
    <div v-else class="card-body stack loose">
      <div class="stack" style="gap: 6px">
        <div class="row" style="gap: 8px; align-items: center; flex-wrap: wrap">
          <h2 style="margin: 0; font-size: 18px">{{ detail.plan.title }}</h2>
          <Badge :value="detail.plan.status" />
        </div>
        <div class="plan-progress" :aria-label="`${detailClosed} of ${detailPhases.length} phases done or skipped`">
          <div class="plan-bar"><span :style="{ width: pct(detailClosed, detailPhases.length) + '%' }" /></div>
          <span class="small muted">{{ detailClosed }}/{{ detailPhases.length }} phases</span>
        </div>
        <p v-if="detail.plan.status === 'draft'" class="hint" style="margin: 0">A draft can't be linked to tasks. Activate it when it is ready.</p>
      </div>

      <SafeMarkdown v-if="detail.plan.description" :text="detail.plan.description" />

      <div class="stack" style="gap: 8px">
        <div class="row" style="justify-content: space-between; align-items: center">
          <h3 class="section-title" style="margin: 0">Phases</h3>
          <template v-if="auth.isOperator">
            <div v-if="editingPhases" class="row" style="gap: 6px">
              <button type="button" class="btn btn-sm" @click="editingPhases = false">Cancel</button>
              <button type="button" class="btn btn-sm btn-primary" :disabled="saving" @click="savePhases"><span v-if="saving" class="spinner" />Save phases</button>
            </div>
            <button v-else type="button" class="btn btn-sm btn-ghost" @click="editPhases"><Icon name="edit" />Edit phases</button>
          </template>
        </div>
        <PlanPhasesEditor v-if="editingPhases" v-model="ePhases" />
        <ol v-else-if="detailPhases.length" class="plan-phases">
          <li v-for="st in detailPhases" :key="st.id" class="plan-phase" :class="st.status">
            <input
              type="checkbox"
              class="plan-check"
              :checked="st.status === 'done'"
              :disabled="!auth.isOperator"
              :aria-label="`Mark ${st.title} ${st.status === 'done' ? 'to do' : 'done'}`"
              @change="setPhase(st, st.status === 'done' ? 'todo' : 'done')"
            />
            <div class="plan-phase-body">
              <div class="plan-phase-title">{{ st.title }}</div>
              <div v-if="st.detail" class="small muted plan-phase-detail">{{ st.detail }}</div>
              <div v-if="st.done_when" class="small plan-phase-done-when"><Icon name="checkCircle" :size="12" /> <span><strong>Done when</strong> {{ st.done_when }}</span></div>
              <ul v-if="phaseTasks(st.id).length" class="plan-phase-tasks">
                <li v-for="t in phaseTasks(st.id)" :key="t.id">
                  <Icon name="tasks" :size="12" /><RouterLink :to="`/tasks/${t.id}`" class="truncate">{{ t.title || t.goal }}</RouterLink><Badge :value="t.status" />
                </li>
              </ul>
              <div v-if="st.note && skipping !== st.id" class="small plan-phase-note"><Icon name="info" :size="12" /> {{ st.note }}</div>
              <form v-if="skipping === st.id" class="row plan-skip" @submit.prevent="setPhase(st, 'skipped', skipNote.trim())">
                <input v-model="skipNote" class="input" maxlength="1000" placeholder="Why is this phase not needed?" :aria-label="`Why ${st.title} is skipped`" autofocus />
                <button type="button" class="btn btn-sm" @click="skipping = null">Cancel</button>
                <button type="submit" class="btn btn-sm btn-primary">Skip phase</button>
              </form>
              <div v-if="st.status !== 'todo'" class="small muted">
                <template v-if="byAgent(st)">
                  <span class="badge outline square">by agent</span>
                  <template v-if="st.done_by_task"> · task <RouterLink :to="`/tasks/${st.done_by_task}`">{{ shortId(st.done_by_task) }}</RouterLink></template>
                </template>
                <template v-else>by a person</template>
                · {{ relTime(st.updated_at) }}
              </div>
            </div>
            <button
              v-if="auth.isOperator && linkable && (st.status === 'todo' || st.status === 'in_progress')"
              type="button"
              class="btn btn-sm btn-ghost plan-phase-task"
              :title="`Create a task that works on ${st.title}`"
              @click="createPhaseTask(st)"
            >
              <Icon name="play" />Task
            </button>
            <select
              class="select plan-phase-status"
              :value="skipping === st.id ? 'skipped' : st.status"
              :disabled="!auth.isOperator"
              :aria-label="`Status of ${st.title}`"
              @change="setPhase(st, ($event.target as HTMLSelectElement).value as PhaseStatus)"
            >
              <option v-for="o in PHASE_STATUSES" :key="o.value" :value="o.value">{{ o.label }}</option>
            </select>
          </li>
        </ol>
        <p v-else class="small muted" style="margin: 0">This plan has no phases yet.</p>
      </div>

      <div class="stack" style="gap: 6px">
        <h3 class="section-title" style="margin: 0">Tasks</h3>
        <ul v-if="detail.tasks?.length" class="plan-tasks">
          <li v-for="t in detail.tasks" :key="t.id">
            <RouterLink :to="`/tasks/${t.id}`" class="truncate">{{ t.title || t.goal }}</RouterLink>
            <span v-if="focusOf(t.id)" class="small muted nowrap" :title="'Works on the phase ' + focusOf(t.id)">phase: {{ focusOf(t.id) }}</span>
            <Badge :value="t.status" />
            <span class="small muted nowrap">{{ relTime(t.created_at) }}</span>
          </li>
        </ul>
        <p v-else class="small muted" style="margin: 0">No task has worked on this plan yet.</p>
      </div>
    </div>
  </section>

  <!-- all plans -->
  <section v-else class="card">
    <div class="card-head">
      <h2><Icon name="list" />Plans</h2>
      <div class="row" style="gap: 8px; align-items: center">
        <div class="plan-filters" role="group" aria-label="Show plans">
          <button v-for="f in FILTERS" :key="f.id" type="button" class="btn btn-sm" :class="{ 'btn-ghost': filter !== f.id }" :aria-pressed="filter === f.id" @click="filter = f.id">
            {{ f.label }}
          </button>
        </div>
        <button v-if="auth.isOperator" type="button" class="btn btn-sm btn-primary" @click="openCreate"><Icon name="plus" />New plan</button>
      </div>
    </div>
    <div v-if="plans === null" class="card-body"><span class="spinner" aria-label="Loading" /></div>
    <div v-else-if="!plans.length" class="card-body">
      <EmptyState title="No plans yet" icon="list" compact>
        Write down a piece of work as phases. Link it to tasks: agents work on the open phases and report their progress.
        <template v-if="auth.isOperator" #actions><button type="button" class="btn btn-primary btn-sm" @click="openCreate"><Icon name="plus" />New plan</button></template>
      </EmptyState>
    </div>
    <ul v-else class="plan-list">
      <li v-for="p in shown" :key="p.id">
        <button type="button" class="plan-row" @click="openPlan(p.id)">
          <span class="plan-row-main">
            <span class="plan-row-title">{{ p.title }}</span>
            <span v-if="p.description" class="small muted truncate">{{ excerpt(p.description) }}</span>
          </span>
          <span class="plan-row-side">
            <Badge :value="p.status" />
            <span class="plan-progress compact">
              <span class="plan-bar"><span :style="{ width: pct(closedOf(p.counts), p.phases) + '%' }" /></span>
              <span class="small muted nowrap">{{ closedOf(p.counts) }}/{{ p.phases }}</span>
            </span>
            <span class="small muted nowrap" :title="`${p.tasks} linked task(s)`"><Icon name="tasks" :size="12" /> {{ p.tasks }}</span>
          </span>
        </button>
      </li>
      <li v-if="!shown.length" class="card-body small muted">No {{ filter === 'open' ? 'open' : filter }} plans.</li>
    </ul>
  </section>

  <Modal :open="formOpen" :title="formMode === 'create' ? 'New plan' : 'Edit plan'" wide :dismissable="!saving" @close="formOpen = false">
    <form id="plan-form" class="stack loose" novalidate @submit.prevent="save">
      <div class="field">
        <label for="pl-title">Title</label>
        <input id="pl-title" v-model="fTitle" class="input" :class="{ invalid: tried && !fTitle.trim() }" maxlength="200" placeholder="e.g. Version endpoint" autofocus />
        <span v-if="tried && !fTitle.trim()" class="error-msg"><Icon name="alert" />Give the plan a title.</span>
      </div>
      <div class="field">
        <label for="pl-desc">Description <span class="opt">(optional, Markdown)</span></label>
        <textarea id="pl-desc" v-model="fDescription" class="textarea" rows="5" maxlength="20000" placeholder="What and why. Agents read this with the phases." />
      </div>
      <div v-if="formMode === 'create'" class="field">
        <span class="label">Phases</span>
        <PlanPhasesEditor v-model="fPhases" />
      </div>
      <label v-if="formMode === 'create'" class="switch">
        <input v-model="fDraft" type="checkbox" />
        Save as draft (not linkable to tasks until activated)
      </label>
    </form>
    <template #footer>
      <button type="button" class="btn" @click="formOpen = false">Cancel</button>
      <button type="submit" form="plan-form" class="btn btn-primary" :disabled="saving">
        <span v-if="saving" class="spinner" />{{ formMode === 'create' ? 'Create plan' : 'Save' }}
      </button>
    </template>
  </Modal>
</template>

<style scoped>
.plan-list {
  list-style: none;
  margin: 0;
  padding: 0;
}
.plan-list > li + li {
  border-top: 1px solid var(--border-primary);
}
.plan-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  width: 100%;
  padding: 12px 16px;
  background: none;
  border: 0;
  color: inherit;
  text-align: left;
  cursor: pointer;
}
.plan-row:hover,
.plan-row:focus-visible {
  background: var(--bg-hover);
}
.plan-row-main {
  display: flex;
  flex-direction: column;
  min-width: 0;
  gap: 2px;
}
.plan-row-title {
  font-weight: 600;
}
.plan-row-side {
  display: flex;
  align-items: center;
  gap: 12px;
  flex: none;
}
.plan-progress {
  display: flex;
  align-items: center;
  gap: 8px;
}
.plan-bar {
  display: block;
  width: 160px;
  height: 6px;
  border-radius: 3px;
  background: var(--border-input);
  overflow: hidden;
}
.plan-progress.compact .plan-bar {
  width: 72px;
}
.plan-bar > span {
  display: block;
  height: 100%;
  background: var(--success-500);
  transition: width 0.2s;
}
.plan-filters {
  display: flex;
  gap: 2px;
}
.plan-phases {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  border: 1px solid var(--border-primary);
  border-radius: 8px;
}
.plan-phase {
  display: flex;
  gap: 10px;
  align-items: flex-start;
  padding: 10px 12px;
}
.plan-phase + .plan-phase {
  border-top: 1px solid var(--border-primary);
}
.plan-phase.done .plan-phase-title,
.plan-phase.skipped .plan-phase-title {
  color: var(--text-tertiary);
  text-decoration: line-through;
}
.plan-check {
  margin-top: 3px;
}
.plan-phase-body {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.plan-phase-detail {
  white-space: pre-wrap;
}
.plan-phase-done-when {
  display: flex;
  gap: 5px;
  align-items: baseline;
  color: var(--text-secondary);
}
.plan-phase-tasks {
  list-style: none;
  margin: 2px 0 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.plan-phase-tasks li {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  min-width: 0;
}
.plan-phase-task {
  flex: none;
}
.plan-phase-note {
  color: var(--text-secondary);
}
.plan-skip {
  gap: 6px;
  margin-top: 4px;
}
.plan-skip .input {
  flex: 1;
  height: 30px;
}
.plan-phase-status {
  width: auto;
  flex: none;
  height: 30px;
  font-size: 12px;
}
.plan-tasks {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.plan-tasks li {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}
.plan-tasks li a {
  flex: 1;
  min-width: 0;
}
@media (max-width: 640px) {
  .plan-row {
    flex-direction: column;
    align-items: flex-start;
  }
  .plan-bar {
    width: 100px;
  }
}
</style>
