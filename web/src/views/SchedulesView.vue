<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api, type Schedule, type ScheduleInput } from '../api'
import { useAuth } from '../stores/auth'
import { useCatalog } from '../stores/catalog'
import { useConfirm } from '../stores/confirm'
import { useToast } from '../stores/toast'
import { taskFormFrom, taskFormValid, taskInput, type TaskForm } from '../lib/taskForm'
import { fmtDate, relTime } from '../lib/format'
import { useNow } from '../lib/now'
import Modal from '../components/Modal.vue'
import TaskFields from '../components/TaskFields.vue'
import Icon from '../components/Icon'
import PageHeader from '../components/PageHeader.vue'
import EmptyState from '../components/EmptyState.vue'
import SkeletonRows from '../components/SkeletonRows.vue'

const auth = useAuth()
const catalog = useCatalog()
const confirm = useConfirm()
const toast = useToast()
const now = useNow()

const schedules = ref<Schedule[]>([])
const loading = ref(true)
const editing = ref<Schedule | null>(null)
const show = ref(false)
const name = ref('')
const cron = ref('0 * * * *')
const enabled = ref(true)
const form = ref<TaskForm>(taskFormFrom())
const saving = ref(false)

const CRON_EXAMPLES = [
  ['*/15 * * * *', 'every 15 minutes'],
  ['0 * * * *', 'hourly'],
  ['0 6 * * *', 'daily at 06:00 UTC'],
  ['0 9 * * 1-5', 'weekdays at 09:00 UTC'],
  ['0 3 * * 0', 'Sundays at 03:00 UTC'],
]

async function load() {
  try {
    schedules.value = (await api.listSchedules()) ?? []
  } catch {
    /* toasted */
  } finally {
    loading.value = false
  }
}

function openNew() {
  editing.value = null
  name.value = ''
  cron.value = '0 6 * * *'
  enabled.value = true
  form.value = taskFormFrom()
  show.value = true
}

function openEdit(s: Schedule) {
  editing.value = s
  name.value = s.name
  cron.value = s.cron
  enabled.value = s.enabled
  form.value = taskFormFrom(s.template)
  show.value = true
}

function body(): ScheduleInput {
  return { name: name.value.trim(), cron: cron.value.trim(), enabled: enabled.value, template: taskInput(form.value) }
}

async function save() {
  if (!name.value.trim() || !cron.value.trim() || !taskFormValid(form.value)) return
  saving.value = true
  try {
    if (editing.value) await api.updateSchedule(editing.value.id, body())
    else await api.createSchedule(body())
    show.value = false
    toast.success(editing.value ? 'Schedule updated' : 'Schedule created')
    load()
  } catch {
    /* toasted */
  } finally {
    saving.value = false
  }
}

async function toggle(s: Schedule) {
  try {
    const updated = await api.updateSchedule(s.id, { name: s.name, cron: s.cron, enabled: !s.enabled, template: s.template })
    Object.assign(s, updated)
  } catch {
    /* toasted */
  }
}

async function run(s: Schedule) {
  try {
    const t = await api.runSchedule(s.id)
    toast.success(`Task queued: ${t.title}`)
    load()
  } catch {
    /* toasted */
  }
}

async function remove(s: Schedule) {
  if (!(await confirm.ask({ title: `Delete schedule ${s.name}?`, message: 'Tasks it already created are kept.', confirmText: `Delete ${s.name}`, danger: true }))) return
  try {
    await api.deleteSchedule(s.id)
    schedules.value = schedules.value.filter((x) => x.id !== s.id)
    toast.success('Schedule deleted')
  } catch {
    /* toasted */
  }
}

function cronHuman(expr: string): string {
  return CRON_EXAMPLES.find(([e]) => e === expr.trim())?.[1] ?? ''
}

function target(s: Schedule) {
  const t = s.template
  if (t.agent_id) return catalog.agentName(t.agent_id)
  if (t.selector?.length) return t.selector.join(', ')
  return 'any agent'
}

onMounted(() => {
  load()
  catalog.loadAgents()
})
</script>

<template>
  <div>
    <PageHeader title="Schedules" subtitle="Recurring tasks on a cron expression (UTC). Each run creates a normal task you can follow on the board.">
      <button v-if="auth.isOperator" type="button" class="btn btn-primary" @click="openNew"><Icon name="plus" />New schedule</button>
    </PageHeader>

    <div class="card">
      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr><th>Name</th><th>Cron</th><th>Enabled</th><th class="hide-mobile">Task</th><th class="hide-mobile">Target</th><th>Next run</th><th class="hide-mobile">Last run</th><th><span class="sr-only">Actions</span></th></tr>
          </thead>
          <tbody>
            <SkeletonRows v-if="loading" :cols="8" :rows="3" />
            <tr v-else-if="!schedules.length">
              <td colspan="8">
                <EmptyState title="No schedules yet" icon="schedules">
                  Run a health check every hour, a report every morning or a cleanup every week.
                  <template v-if="auth.isOperator" #actions><button type="button" class="btn btn-primary" @click="openNew"><Icon name="plus" />New schedule</button></template>
                </EmptyState>
              </td>
            </tr>
            <tr v-for="s in schedules" :key="s.id">
              <td class="cell-title">{{ s.name }}</td>
              <td><code class="label-chip">{{ s.cron }}</code><div class="cell-sub">{{ cronHuman(s.cron) }}</div></td>
              <td>
                <label class="switch">
                  <input type="checkbox" :checked="s.enabled" :disabled="!auth.isOperator" :aria-label="`Enable ${s.name}`" @change="toggle(s)" />
                </label>
              </td>
              <td class="truncate hide-mobile" style="max-width: 240px">{{ s.template.title || s.template.goal }}</td>
              <td class="nowrap hide-mobile">{{ target(s) }}</td>
              <td class="nowrap" :title="fmtDate(s.next_run_at)">{{ s.enabled ? relTime(s.next_run_at, now) : '—' }}</td>
              <td class="nowrap hide-mobile" :title="fmtDate(s.last_run_at)">{{ relTime(s.last_run_at, now) }}</td>
              <td class="right nowrap">
                <template v-if="auth.isOperator">
                  <div class="row end" style="gap: 4px">
                    <button type="button" class="btn btn-sm" @click="run(s)"><Icon name="play" />Run now</button>
                    <button type="button" class="btn btn-sm btn-ghost btn-icon" :aria-label="`Edit ${s.name}`" title="Edit" @click="openEdit(s)"><Icon name="edit" /></button>
                    <button type="button" class="btn btn-sm btn-ghost btn-icon btn-danger-ghost" :aria-label="`Delete ${s.name}`" title="Delete" @click="remove(s)"><Icon name="trash" /></button>
                  </div>
                </template>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <Modal :open="show" :title="editing ? `Edit ${editing.name}` : 'New schedule'" wide :dismissable="!saving" @close="show = false">
      <form id="schedule-form" class="stack" @submit.prevent="save">
        <div class="grid-2">
          <div class="field">
            <label for="sc-name">Name <span class="req">*</span></label>
            <input id="sc-name" v-model="name" class="input" required maxlength="120" />
          </div>
          <div class="field">
            <label for="sc-cron">Cron <span class="req">*</span></label>
            <input id="sc-cron" v-model="cron" class="input mono" required placeholder="0 6 * * *" aria-describedby="sc-cron-help" />
            <span id="sc-cron-help" class="hint">minute hour day-of-month month day-of-week, in UTC.</span>
          </div>
          <div class="field span-all">
            <div class="chips">
              <button v-for="[expr, label] in CRON_EXAMPLES" :key="expr" type="button" class="chip-toggle" :class="{ on: cron === expr }" @click="cron = expr">
                <span class="mono">{{ expr }}</span><span class="muted">{{ label }}</span>
              </button>
            </div>
          </div>
          <label class="switch span-all"><input v-model="enabled" type="checkbox" />Enabled: runs on schedule</label>
        </div>
        <div class="section-title" style="margin-top: 8px">Task each run creates</div>
        <TaskFields v-model="form" id-prefix="sc" />
      </form>
      <template #footer>
        <button type="button" class="btn" @click="show = false">Cancel</button>
        <button type="submit" form="schedule-form" class="btn btn-primary" :disabled="saving || !name.trim() || !cron.trim() || !taskFormValid(form)">
          <span v-if="saving" class="spinner" />{{ editing ? 'Save changes' : 'Create schedule' }}
        </button>
      </template>
    </Modal>
  </div>
</template>
