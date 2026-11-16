<!--
  Copyright (c) 2026 Jonas Kaninda.
  SPDX-License-Identifier: MIT
-->
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api, type ChangeWindow, type WindowInput, type WindowKind } from '../../api'
import { useAuth } from '../../stores/auth'
import { useConfirm } from '../../stores/confirm'
import { useToast } from '../../stores/toast'
import { fmtDate, splitList } from '../../lib/format'
import Badge from '../../components/Badge.vue'
import EmptyState from '../../components/EmptyState.vue'
import SkeletonRows from '../../components/SkeletonRows.vue'
import Modal from '../../components/Modal.vue'
import Icon from '../../components/Icon'

const auth = useAuth()
const confirm = useConfirm()
const toast = useToast()

const windows = ref<ChangeWindow[]>([])
const loading = ref(true)
const show = ref(false)
const editing = ref<ChangeWindow | null>(null)
const saving = ref(false)

interface Form {
  name: string
  kind: WindowKind
  recurring: boolean
  cron: string
  duration_min: number
  starts_at: string
  ends_at: string
  timezone: string
  labels: string
  enabled: boolean
}
const blank = (): Form => ({ name: '', kind: 'window', recurring: true, cron: '0 22 * * 1-5', duration_min: 120, starts_at: '', ends_at: '',
  timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC', labels: '', enabled: true })
const form = ref<Form>(blank())

// datetime-local works in the browser's zone; the API takes ISO instants.
const toLocal = (s: string | null) => (s ? new Date(new Date(s).getTime() - new Date(s).getTimezoneOffset() * 60_000).toISOString().slice(0, 16) : '')
const fromLocal = (s: string) => (s ? new Date(s).toISOString() : null)

async function load() {
  try {
    windows.value = (await api.listWindows()) ?? []
  } catch {
    /* toasted */
  } finally {
    loading.value = false
  }
}

function openNew(kind: WindowKind) {
  editing.value = null
  form.value = { ...blank(), kind, recurring: kind === 'window' }
  show.value = true
}

function openEdit(w: ChangeWindow) {
  editing.value = w
  form.value = { name: w.name, kind: w.kind, recurring: !!w.cron, cron: w.cron || '0 22 * * 1-5', duration_min: w.duration_min || 120,
    starts_at: toLocal(w.starts_at), ends_at: toLocal(w.ends_at), timezone: w.timezone || 'UTC', labels: (w.labels ?? []).join(', '), enabled: w.enabled }
  show.value = true
}

const valid = computed(() => {
  const f = form.value
  if (!f.name.trim()) return false
  return f.recurring ? !!f.cron.trim() && f.duration_min > 0 : !!f.starts_at && !!f.ends_at && f.ends_at > f.starts_at
})

function body(f: Form): WindowInput {
  return {
    name: f.name.trim(), kind: f.kind, timezone: f.timezone.trim(), labels: splitList(f.labels), enabled: f.enabled,
    cron: f.recurring ? f.cron.trim() : '', duration_min: f.recurring ? f.duration_min : 0,
    starts_at: f.recurring ? null : fromLocal(f.starts_at), ends_at: f.recurring ? null : fromLocal(f.ends_at),
  }
}

async function save() {
  if (!valid.value) return
  saving.value = true
  try {
    if (editing.value) await api.updateWindow(editing.value.id, body(form.value))
    else await api.createWindow(body(form.value))
    show.value = false
    toast.success(editing.value ? 'Saved' : form.value.kind === 'freeze' ? 'Freeze added' : 'Window added')
    load()
  } catch {
    /* toasted */
  } finally {
    saving.value = false
  }
}

async function toggle(w: ChangeWindow) {
  try {
    Object.assign(w, await api.updateWindow(w.id, { ...w, enabled: !w.enabled }))
  } catch {
    /* toasted */
  }
}

async function remove(w: ChangeWindow) {
  if (!(await confirm.ask({ title: `Delete ${w.name}?`, confirmText: 'Delete', danger: true }))) return
  try {
    await api.deleteWindow(w.id)
    windows.value = windows.value.filter((x) => x.id !== w.id)
  } catch {
    /* toasted */
  }
}

function when(w: ChangeWindow) {
  return w.cron ? `${w.cron} for ${w.duration_min} min (${w.timezone || 'UTC'})` : `${fmtDate(w.starts_at)} – ${fmtDate(w.ends_at)}`
}

onMounted(load)
</script>

<template>
  <div class="card">
    <div class="card-head">
      <h2><Icon name="clock" />Change windows and freezes</h2>
      <div v-if="auth.isAdmin" class="row" style="gap: 6px">
        <button type="button" class="btn btn-sm" @click="openNew('freeze')"><Icon name="lock" />Add freeze</button>
        <button type="button" class="btn btn-sm btn-primary" @click="openNew('window')"><Icon name="plus" />Add window</button>
      </div>
    </div>
    <div class="card-body small muted" style="padding-bottom: 0">
      During a freeze, grants pause and every high-risk call or change plan needs a person, whatever the autonomy level. Grants marked
      “in windows only” work only while a window is open.
    </div>
    <div class="table-wrap">
      <table class="table">
        <thead>
          <tr><th>Name</th><th>Kind</th><th>When</th><th class="hide-mobile">Agents</th><th>Now</th><th>Enabled</th><th><span class="sr-only">Actions</span></th></tr>
        </thead>
        <tbody>
          <SkeletonRows v-if="loading" :cols="7" :rows="2" />
          <tr v-else-if="!windows.length">
            <td colspan="7"><EmptyState title="No windows or freezes" icon="clock">Add a nightly maintenance window, or a freeze for a launch week.</EmptyState></td>
          </tr>
          <tr v-for="w in windows" :key="w.id">
            <td class="cell-title">{{ w.name }}</td>
            <td><Badge :value="w.kind" /></td>
            <td class="small mono">{{ when(w) }}</td>
            <td class="hide-mobile">{{ w.labels?.length ? w.labels.join(', ') : 'every agent' }}</td>
            <td><Badge :value="w.active ? 'active' : 'inactive'" /></td>
            <td><label class="switch"><input type="checkbox" :checked="w.enabled" :disabled="!auth.isAdmin" :aria-label="`Enable ${w.name}`" @change="toggle(w)" /></label></td>
            <td class="right nowrap">
              <template v-if="auth.isAdmin">
                <button type="button" class="btn btn-sm btn-ghost btn-icon" :aria-label="`Edit ${w.name}`" title="Edit" @click="openEdit(w)"><Icon name="edit" /></button>
                <button type="button" class="btn btn-sm btn-ghost btn-icon btn-danger-ghost" :aria-label="`Delete ${w.name}`" title="Delete" @click="remove(w)"><Icon name="trash" /></button>
              </template>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <Modal :open="show" :title="editing ? `Edit ${editing.name}` : form.kind === 'freeze' ? 'New freeze' : 'New change window'" :dismissable="!saving" @close="show = false">
      <form id="window-form" class="stack" @submit.prevent="save">
        <div class="field">
          <label for="w-name">Name <span class="req">*</span></label>
          <input id="w-name" v-model="form.name" class="input" maxlength="120" required :placeholder="form.kind === 'freeze' ? 'Black Friday freeze' : 'Weeknight maintenance'" />
        </div>
        <div class="grid-2">
          <div class="field">
            <label for="w-kind">Kind</label>
            <select id="w-kind" v-model="form.kind" class="select"><option value="window">Change window</option><option value="freeze">Freeze</option></select>
          </div>
          <div class="field">
            <label for="w-rec">Schedule</label>
            <select id="w-rec" v-model="form.recurring" class="select"><option :value="true">Recurring</option><option :value="false">One-off</option></select>
          </div>
        </div>
        <div v-if="form.recurring" class="grid-2">
          <div class="field">
            <label for="w-cron">Starts (cron) <span class="req">*</span></label>
            <input id="w-cron" v-model="form.cron" class="input mono" placeholder="0 22 * * 1-5" />
          </div>
          <div class="field">
            <label for="w-dur">Lasts (minutes) <span class="req">*</span></label>
            <input id="w-dur" v-model.number="form.duration_min" class="input" type="number" min="1" max="10080" />
          </div>
        </div>
        <div v-else class="grid-2">
          <div class="field">
            <label for="w-start">Starts <span class="req">*</span></label>
            <input id="w-start" v-model="form.starts_at" class="input" type="datetime-local" />
          </div>
          <div class="field">
            <label for="w-end">Ends <span class="req">*</span></label>
            <input id="w-end" v-model="form.ends_at" class="input" type="datetime-local" />
          </div>
        </div>
        <div class="grid-2">
          <div class="field">
            <label for="w-tz">Time zone</label>
            <input id="w-tz" v-model="form.timezone" class="input" placeholder="Europe/Berlin" :disabled="!form.recurring" />
            <span class="hint">For the cron expression. One-off times use your browser's zone.</span>
          </div>
          <div class="field">
            <label for="w-labels">Agent labels</label>
            <input id="w-labels" v-model="form.labels" class="input" placeholder="empty: every agent" />
          </div>
        </div>
        <label class="switch"><input v-model="form.enabled" type="checkbox" />Enabled</label>
      </form>
      <template #footer>
        <button type="button" class="btn" @click="show = false">Cancel</button>
        <button type="submit" form="window-form" class="btn btn-primary" :disabled="saving || !valid"><span v-if="saving" class="spinner" />Save</button>
      </template>
    </Modal>
  </div>
</template>
