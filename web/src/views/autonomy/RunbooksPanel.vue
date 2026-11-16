<!--
  Copyright (c) 2026 Jonas Kaninda.
  SPDX-License-Identifier: MIT
-->
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, type Runbook, type RunbookInput, type RunbookParam } from '../../api'
import { useAuth } from '../../stores/auth'
import { useCatalog } from '../../stores/catalog'
import { useConfirm } from '../../stores/confirm'
import { useToast } from '../../stores/toast'
import { describeConstraint, GRANT_DURATIONS, rowConstraint, type ArgMode, type ArgRow } from '../../lib/grants'
import { prettyJSON, splitList } from '../../lib/format'
import Badge from '../../components/Badge.vue'
import EmptyState from '../../components/EmptyState.vue'
import SkeletonRows from '../../components/SkeletonRows.vue'
import Modal from '../../components/Modal.vue'
import Icon from '../../components/Icon'

const emit = defineEmits<{ (e: 'granted'): void }>()

const auth = useAuth()
const catalog = useCatalog()
const confirm = useConfirm()
const toast = useToast()
const route = useRoute()
const router = useRouter()

const runbooks = ref<Runbook[]>([])
const loading = ref(true)

interface ParamRow extends ArgRow {
  description: string
}
const show = ref(false)
const editing = ref<Runbook | null>(null)
const name = ref('')
const description = ref('')
const planText = ref('')
const params = ref<ParamRow[]>([])
const enabled = ref(true)
const sourceChange = ref<string | null>(null)
const saving = ref(false)

const planJSON = computed<unknown | null>(() => {
  try {
    return JSON.parse(planText.value)
  } catch {
    return null
  }
})
const placeholders = computed(() => [...new Set([...planText.value.matchAll(/\{\{\s*([a-z][a-z0-9_]*)\s*\}\}/g)].map((m) => m[1]))])
const missing = computed(() => placeholders.value.filter((p) => !params.value.some((r) => r.key === p)))
const valid = computed(() => name.value.trim() !== '' && planJSON.value !== null && !missing.value.length && params.value.every((r) => rowConstraint(r) !== null))

function paramRow(p?: RunbookParam, key = ''): ParamRow {
  const b = p?.bounds ?? {}
  let mode: ArgMode = 'one_of'
  let value = ''
  if (b.equals !== undefined) [mode, value] = ['equals', typeof b.equals === 'string' ? b.equals : JSON.stringify(b.equals)]
  else if (b.glob === '*') mode = 'any'
  else if (b.glob) [mode, value] = ['glob', b.glob]
  else if (b.one_of) value = b.one_of.join(', ')
  else if (b.min !== undefined || b.max !== undefined) mode = 'range'
  return { key: p?.name ?? key, description: p?.description ?? '', mode, value, min: b.min?.toString() ?? '', max: b.max?.toString() ?? '' }
}

function openEditor(rb: Runbook | null, draft?: RunbookInput) {
  editing.value = rb
  const src = rb ?? draft
  name.value = src?.name ?? ''
  description.value = src?.description ?? ''
  planText.value = src ? prettyJSON(src.plan) : '{\n  "steps": [],\n  "verify": [],\n  "rollback": []\n}'
  params.value = (src?.params ?? []).map((p) => paramRow(p))
  enabled.value = rb ? rb.enabled : true
  sourceChange.value = draft?.source_change_id ?? rb?.source_change_id ?? null
  show.value = true
}

function addMissing() {
  for (const p of missing.value) params.value.push(paramRow(undefined, p))
}

async function save() {
  if (!valid.value) return
  const body: RunbookInput = {
    name: name.value.trim(), description: description.value.trim(), plan: planJSON.value, enabled: enabled.value, source_change_id: sourceChange.value,
    params: params.value.map((r) => ({ name: r.key, description: r.description.trim(), bounds: rowConstraint(r)! })),
  }
  saving.value = true
  try {
    if (editing.value) await api.updateRunbook(editing.value.id, body)
    else await api.createRunbook(body)
    show.value = false
    toast.success(editing.value ? 'Runbook saved; grants for the previous version stop matching if its calls or bounds changed' : 'Runbook added')
    load()
  } catch {
    /* toasted */
  } finally {
    saving.value = false
  }
}

async function remove(rb: Runbook) {
  if (!(await confirm.ask({ title: `Delete runbook ${rb.name}?`, message: 'Its grants are revoked.', confirmText: 'Delete', danger: true }))) return
  try {
    await api.deleteRunbook(rb.id)
    runbooks.value = runbooks.value.filter((x) => x.id !== rb.id)
  } catch {
    /* toasted */
  }
}

const granting = ref<Runbook | null>(null)
const gScope = ref<'agent' | 'labels'>('agent')
const gAgent = ref('')
const gLabels = ref('')
const gHours = ref(24)
const gUses = ref(0)
const gWindowed = ref(false)
const gReason = ref('')
const gValid = computed(() => gReason.value.trim() !== '' && (gScope.value === 'agent' ? !!gAgent.value : splitList(gLabels.value).length > 0))

function openGrant(rb: Runbook) {
  granting.value = rb
  gScope.value = 'agent'
  gAgent.value = ''
  gLabels.value = ''
  gHours.value = 24
  gUses.value = 0
  gWindowed.value = false
  gReason.value = ''
}

async function grant() {
  const rb = granting.value
  if (!rb || !gValid.value) return
  saving.value = true
  try {
    await api.createGrant({
      tool: 'change_run', runbook_id: rb.id, max_risk: 'high', expires_in_hours: gHours.value, max_uses: gUses.value, windowed: gWindowed.value,
      reason: gReason.value.trim(), agent_id: gScope.value === 'agent' ? gAgent.value : null, labels: gScope.value === 'labels' ? splitList(gLabels.value) : [],
    })
    granting.value = null
    toast.success(`Runbook ${rb.name} granted`)
    emit('granted')
  } catch {
    /* toasted */
  } finally {
    saving.value = false
  }
}

async function load() {
  try {
    runbooks.value = (await api.listRunbooks()) ?? []
  } catch {
    /* toasted */
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  load()
  catalog.loadAgents()
  const from = route.query.from_change
  if (typeof from === 'string' && auth.isAdmin) {
    router.replace({ query: { ...route.query, from_change: undefined } })
    try {
      openEditor(null, await api.runbookDraft(from))
    } catch {
      /* toasted */
    }
  }
})
</script>

<template>
  <div class="card">
    <div class="card-head">
      <h2><Icon name="skills" />Runbooks</h2>
      <button v-if="auth.isAdmin" type="button" class="btn btn-sm btn-primary" @click="openEditor(null)"><Icon name="plus" />New runbook</button>
    </div>
    <div class="card-body small muted" style="padding-bottom: 0">
      A runbook is a change plan with bounded <span class="mono">{{ '{' + '{parameters}' + '}' }}</span>. Grant it, and a matching plan runs without waiting for a person;
      its checks and rollback still run. Start one from a finished change with “Save as runbook”.
    </div>
    <div class="table-wrap">
      <table class="table">
        <thead>
          <tr><th>Name</th><th>Parameters</th><th>Version</th><th>Enabled</th><th><span class="sr-only">Actions</span></th></tr>
        </thead>
        <tbody>
          <SkeletonRows v-if="loading" :cols="5" :rows="2" />
          <tr v-else-if="!runbooks.length">
            <td colspan="5"><EmptyState title="No runbooks yet" icon="skills">Open a change that worked and save it as a runbook.</EmptyState></td>
          </tr>
          <tr v-for="rb in runbooks" :key="rb.id">
            <td class="cell-title">{{ rb.name }}<div v-if="rb.description" class="cell-sub">{{ rb.description }}</div></td>
            <td class="small">
              <div v-for="p in rb.params ?? []" :key="p.name"><span class="mono">{{ p.name }}</span> {{ describeConstraint(p.bounds) }}</div>
              <span v-if="!rb.params?.length" class="muted">none</span>
            </td>
            <td>v{{ rb.version }}</td>
            <td><Badge :value="rb.enabled ? 'active' : 'inactive'" /></td>
            <td class="right nowrap">
              <button v-if="auth.isOperator && rb.enabled" type="button" class="btn btn-sm" @click="openGrant(rb)"><Icon name="key" />Grant</button>
              <template v-if="auth.isAdmin">
                <button type="button" class="btn btn-sm btn-ghost btn-icon" :aria-label="`Edit ${rb.name}`" title="Edit" @click="openEditor(rb)"><Icon name="edit" /></button>
                <button type="button" class="btn btn-sm btn-ghost btn-icon btn-danger-ghost" :aria-label="`Delete ${rb.name}`" title="Delete" @click="remove(rb)"><Icon name="trash" /></button>
              </template>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <Modal :open="show" :title="editing ? `Edit ${editing.name}` : 'New runbook'" wide :dismissable="!saving" @close="show = false">
      <form id="runbook-form" class="stack" @submit.prevent="save">
        <div class="grid-2">
          <div class="field">
            <label for="rb-name">Name <span class="req">*</span></label>
            <input id="rb-name" v-model="name" class="input" maxlength="120" required />
          </div>
          <div class="field">
            <label for="rb-desc">Description</label>
            <input id="rb-desc" v-model="description" class="input" maxlength="1000" />
          </div>
        </div>
        <div class="field">
          <label for="rb-plan">Plan <span class="req">*</span></label>
          <textarea id="rb-plan" v-model="planText" class="input mono" rows="14" spellcheck="false" :class="{ invalid: planJSON === null }" />
          <span class="hint">steps, verify and rollback calls, as in change_run. Replace values that may vary with <span class="mono">{{ '{' + '{name}' + '}' }}</span>; title and reason are left to the agent.</span>
        </div>
        <div class="field">
          <span class="label">Parameter bounds</span>
          <div v-if="missing.length" class="row wrap small" style="gap: 8px">
            <span class="error-msg"><Icon name="alert" />No bounds for {{ missing.join(', ') }}</span>
            <button type="button" class="btn btn-sm" @click="addMissing">Add them</button>
          </div>
          <div v-for="(r, i) in params" :key="i" class="row wrap" style="gap: 8px; margin-bottom: 6px">
            <code class="label-chip" style="min-width: 100px">{{ r.key }}</code>
            <select v-model="r.mode" class="select sm" :aria-label="`Bounds for ${r.key}`" style="width: 130px">
              <option value="one_of">one of</option>
              <option value="glob">matches glob</option>
              <option value="range">in range</option>
              <option value="equals">exactly</option>
              <option value="any">any value</option>
            </select>
            <template v-if="r.mode === 'range'">
              <input v-model="r.min" class="input sm" type="number" placeholder="min" style="width: 90px" :aria-label="`${r.key} minimum`" />
              <input v-model="r.max" class="input sm" type="number" placeholder="max" style="width: 90px" :aria-label="`${r.key} maximum`" />
            </template>
            <input v-else-if="r.mode !== 'any'" v-model="r.value" class="input sm mono" style="width: 180px" :aria-label="`${r.key} value`" />
            <input v-model="r.description" class="input sm grow" placeholder="what it is" :aria-label="`${r.key} description`" />
            <button type="button" class="btn btn-sm btn-ghost btn-icon" :aria-label="`Remove ${r.key}`" @click="params.splice(i, 1)"><Icon name="x" /></button>
          </div>
          <span v-if="!params.length && !missing.length" class="hint">No placeholders: the plan must match exactly.</span>
        </div>
        <label class="switch"><input v-model="enabled" type="checkbox" />Enabled</label>
      </form>
      <template #footer>
        <button type="button" class="btn" @click="show = false">Cancel</button>
        <button type="submit" form="runbook-form" class="btn btn-primary" :disabled="saving || !valid"><span v-if="saving" class="spinner" />Save runbook</button>
      </template>
    </Modal>

    <Modal :open="!!granting" :title="`Grant runbook ${granting?.name ?? ''}`" :dismissable="!saving" @close="granting = null">
      <form id="rb-grant-form" class="stack" @submit.prevent="grant">
        <p class="small muted" style="margin: 0">
          A change plan that matches this runbook version and its bounds runs without asking. Editing the runbook ends the grant.
        </p>
        <div class="grid-2">
          <div class="field">
            <label for="rg-scope">Agents</label>
            <select id="rg-scope" v-model="gScope" class="select"><option value="agent">One agent</option><option value="labels">By labels</option></select>
          </div>
          <div v-if="gScope === 'agent'" class="field">
            <label for="rg-agent">Agent</label>
            <select id="rg-agent" v-model="gAgent" class="select">
              <option value="" disabled>Pick an agent</option>
              <option v-for="a in catalog.agents" :key="a.id" :value="a.id">{{ a.name }}</option>
            </select>
          </div>
          <div v-else class="field">
            <label for="rg-labels">Labels</label>
            <input id="rg-labels" v-model="gLabels" class="input" placeholder="prod, eu" />
          </div>
          <div class="field">
            <label for="rg-hours">Expires after</label>
            <select id="rg-hours" v-model.number="gHours" class="select"><option v-for="[h, l] in GRANT_DURATIONS" :key="h" :value="h">{{ l }}</option></select>
          </div>
          <div class="field">
            <label for="rg-uses">Maximum uses</label>
            <input id="rg-uses" v-model.number="gUses" class="input" type="number" min="0" />
          </div>
        </div>
        <label class="switch"><input v-model="gWindowed" type="checkbox" />Only during change windows</label>
        <div class="field">
          <label for="rg-reason">Why is this safe? <span class="req">*</span></label>
          <input id="rg-reason" v-model="gReason" class="input" maxlength="500" />
        </div>
      </form>
      <template #footer>
        <button type="button" class="btn" @click="granting = null">Cancel</button>
        <button type="submit" form="rb-grant-form" class="btn btn-primary" :disabled="saving || !gValid"><span v-if="saving" class="spinner" />Grant</button>
      </template>
    </Modal>
  </div>
</template>
