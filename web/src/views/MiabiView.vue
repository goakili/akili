<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { api, ApiError, AUTONOMY_LEVELS, type Autonomy, type MiabiWatch, type MiabiWatchInput, type MiabiWorkspace } from '../api'
import { useAuth } from '../stores/auth'
import { useCatalog } from '../stores/catalog'
import { useCoder } from '../stores/coder'
import { useConfirm } from '../stores/confirm'
import { useToast } from '../stores/toast'
import { fmtDate, relTime, safeUrl, splitList } from '../lib/format'
import { useNow } from '../lib/now'
import Modal from '../components/Modal.vue'
import AutonomySelect from '../components/AutonomySelect.vue'
import PageHeader from '../components/PageHeader.vue'
import EmptyState from '../components/EmptyState.vue'
import SkeletonRows from '../components/SkeletonRows.vue'
import Icon from '../components/Icon'

const auth = useAuth()
const catalog = useCatalog()
const coder = useCoder()
const confirm = useConfirm()
const toast = useToast()
const route = useRoute()
const now = useNow()

const watches = ref<MiabiWatch[]>([])
const loading = ref(true)
const busy = ref<string | null>(null)
/** ?watch=<id> (from a task's Miabi chip) highlights that row. */
const highlight = computed(() => (typeof route.query.watch === 'string' ? route.query.watch : ''))

const miabiIntegrations = computed(() => coder.integrations.filter((i) => i.kind === 'miabi'))
const agents = computed(() => catalog.agents.filter((a) => a.status !== 'revoked'))

async function load() {
  try {
    watches.value = (await api.listMiabiWatches()) ?? []
  } catch {
    /* toasted */
  } finally {
    loading.value = false
  }
}

/** workspace/app, or just the app when the watch names no workspace. */
const watchLabel = (w: MiabiWatch) => (w.workspace ? `${w.workspace}/${w.app}` : w.app)

/** Enabled workspaces per integration, loaded when the form picks one. */
const workspacesBy = ref<Record<string, MiabiWorkspace[]>>({})
const wsLoading = ref(false)

async function loadWorkspaces(integrationId: string) {
  if (!integrationId || workspacesBy.value[integrationId]) return
  wsLoading.value = true
  try {
    const all = (await api.listMiabiWorkspaces(integrationId, { quiet: true })) ?? []
    workspacesBy.value[integrationId] = all.filter((w) => w.enabled)
  } catch {
    workspacesBy.value[integrationId] = []
  } finally {
    wsLoading.value = false
  }
}

function integrationName(id: string): string {
  return coder.integrations.find((i) => i.id === id)?.name ?? id
}

function target(w: MiabiWatch): string {
  if (w.agent_id) return catalog.agentName(w.agent_id)
  return w.selector?.length ? `agents labelled ${w.selector.join(', ')}` : 'any agent'
}

type Target = 'agent' | 'labels' | 'any'
interface Form {
  integration_id: string
  workspace: string
  app: string
  target: Target
  agent_id: string
  selector: string
  verify_deploys: boolean
  triage_failures: boolean
  databases: boolean
  health_url: string
  autonomy: Autonomy
  instructions: string
}

const show = ref(false)
const editing = ref<MiabiWatch | null>(null)
const saving = ref(false)
const tried = ref(false)
const formError = ref('')
const form = ref<Form>(blank())

function blank(): Form {
  return {
    integration_id: miabiIntegrations.value[0]?.id ?? '',
    workspace: '',
    app: '',
    target: 'any',
    agent_id: '',
    selector: '',
    verify_deploys: true,
    triage_failures: true,
    databases: false,
    health_url: '',
    autonomy: 2,
    instructions: '',
  }
}

function formFrom(w: MiabiWatch): Form {
  return {
    integration_id: w.integration_id,
    workspace: w.workspace ?? '',
    app: w.app,
    target: w.agent_id ? 'agent' : w.selector?.length ? 'labels' : 'any',
    agent_id: w.agent_id ?? '',
    selector: (w.selector ?? []).join(', '),
    verify_deploys: w.verify_deploys,
    triage_failures: w.triage_failures,
    databases: !!w.databases,
    health_url: w.health_url,
    autonomy: w.autonomy,
    instructions: w.instructions,
  }
}

function inputFrom(f: Form): MiabiWatchInput {
  return {
    integration_id: f.integration_id,
    workspace: f.workspace,
    app: f.app.trim(),
    agent_id: f.target === 'agent' ? f.agent_id || null : null,
    selector: f.target === 'labels' ? splitList(f.selector) : [],
    verify_deploys: f.verify_deploys,
    triage_failures: f.triage_failures,
    databases: f.databases,
    health_url: f.health_url.trim(),
    autonomy: f.autonomy,
    instructions: f.instructions,
  }
}

const errors = computed(() => {
  const f = form.value
  const e: Record<string, string> = {}
  if (!f.integration_id) e.integration = 'Pick a Miabi integration.'
  if (f.integration_id && !f.workspace && formWorkspaces.value.length > 1) e.workspace = 'This integration has several enabled workspaces: pick one.'
  if (!f.app.trim()) e.app = 'Enter an app name, or * for every app.'
  if (f.target === 'agent' && !f.agent_id) e.agent = 'Pick the agent.'
  if (f.target === 'labels' && !splitList(f.selector).length) e.labels = 'Enter at least one label.'
  if (f.health_url.trim() && !/^https?:\/\//i.test(f.health_url.trim())) e.health_url = 'Use a full http(s):// URL.'
  return e
})
const err = (k: string) => (tried.value ? errors.value[k] : '')
const nothingWatched = computed(() => !form.value.verify_deploys && !form.value.triage_failures && !form.value.databases)
const formWorkspaces = computed(() => workspacesBy.value[form.value.integration_id] ?? [])

// Keeps the workspace choice valid for the picked integration; a single enabled one is preselected.
async function pickWorkspace(id: string) {
  await loadWorkspaces(id)
  if (form.value.integration_id !== id) return
  const enabled = formWorkspaces.value
  if (editing.value) return
  if (form.value.workspace && !enabled.some((w) => w.name === form.value.workspace)) form.value.workspace = ''
  if (!form.value.workspace && enabled.length === 1) form.value.workspace = enabled[0].name
}

watch(() => form.value.integration_id, pickWorkspace)

function openNew() {
  editing.value = null
  form.value = blank()
  pickWorkspace(form.value.integration_id)
  tried.value = false
  formError.value = ''
  show.value = true
}

function openEdit(w: MiabiWatch) {
  editing.value = w
  form.value = formFrom(w)
  pickWorkspace(w.integration_id)
  tried.value = false
  formError.value = ''
  show.value = true
}

async function save() {
  tried.value = true
  formError.value = ''
  if (Object.keys(errors.value).length) return
  saving.value = true
  try {
    const body = inputFrom(form.value)
    if (editing.value) {
      const w = await api.updateMiabiWatch(editing.value.id, body, { quiet: true })
      const i = watches.value.findIndex((x) => x.id === w.id)
      if (i >= 0) watches.value[i] = w
      toast.success(`Watch on ${watchLabel(w)} saved`)
    } else {
      const w = await api.createMiabiWatch(body, { quiet: true })
      watches.value = [...watches.value, w].sort((a, b) => watchLabel(a).localeCompare(watchLabel(b)))
      toast.success(`Watching ${watchLabel(w)}`)
    }
    show.value = false
  } catch (e) {
    formError.value = e instanceof ApiError ? e.message : 'Saving failed.'
  } finally {
    saving.value = false
  }
}

async function remove(w: MiabiWatch) {
  const ok = await confirm.ask({
    title: `Stop watching ${watchLabel(w)}?`,
    message: 'Its deploys are no longer verified and failures no longer triaged, and its live event stream closes if nothing else watches the workspace. Tasks it already created are kept; nothing changes on Miabi.',
    confirmText: 'Stop watching',
    danger: true,
  })
  if (!ok) return
  busy.value = w.id
  try {
    await api.deleteMiabiWatch(w.id)
    watches.value = watches.value.filter((x) => x.id !== w.id)
    toast.success('Watch deleted')
  } catch {
    /* toasted */
  } finally {
    busy.value = null
  }
}

onMounted(() => {
  load()
  catalog.loadAgents()
  coder.loadIntegrations()
})
</script>

<template>
  <div>
    <PageHeader title="Miabi" subtitle="Watched Miabi apps, followed live. Every successful deploy is verified and rolled back (through an approved change plan) if the app is unhealthy; failed deploys, drift, crashes and failed database backups open triage tasks.">
      <button v-if="auth.isAdmin" type="button" class="btn btn-primary" @click="openNew"><Icon name="plus" />Watch an app</button>
    </PageHeader>

    <div v-if="auth.isAdmin && !loading && !miabiIntegrations.length" class="banner info" style="margin-bottom: 16px">
      <Icon name="plug" />
      <div class="banner-body">Watching an app needs a <strong>Miabi integration</strong> (URL and an API key) with at least one enabled workspace. Events are followed live; a webhook is optional.</div>
      <RouterLink to="/integrations" class="btn btn-sm"><Icon name="plug" />Add integration</RouterLink>
    </div>

    <div class="card">
      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr>
              <th>App</th><th class="hide-mobile">Integration</th><th class="hide-mobile">Runs on</th><th>Watches</th><th class="hide-mobile">Autonomy</th><th class="hide-mobile">Last event</th>
              <th v-if="auth.isAdmin"><span class="sr-only">Actions</span></th>
            </tr>
          </thead>
          <tbody>
            <SkeletonRows v-if="loading" :cols="auth.isAdmin ? 7 : 6" :rows="3" />
            <tr v-else-if="!watches.length">
              <td :colspan="auth.isAdmin ? 7 : 6">
                <EmptyState title="No watched apps yet" icon="layers">
                  Watch a Miabi app and every deploy gets checked: an unhealthy release gets a rollback plan for you to approve, and failed deploys or crashed containers become triage tasks.
                  <template v-if="auth.isAdmin && miabiIntegrations.length" #actions><button type="button" class="btn btn-primary" @click="openNew"><Icon name="plus" />Watch an app</button></template>
                </EmptyState>
              </td>
            </tr>
            <tr v-for="w in loading ? [] : watches" :key="w.id" :class="{ 'row-hl': highlight === w.id }">
              <td>
                <div class="cell-title row" style="gap: 6px"><Icon name="layers" :size="15" /><span><span v-if="w.workspace" class="muted">{{ w.workspace }}/</span>{{ w.app }}</span></div>
                <div v-if="safeUrl(w.health_url)" class="cell-sub mono truncate" style="max-width: 260px" :title="w.health_url">{{ w.health_url }}</div>
                <div v-else-if="w.instructions" class="cell-sub truncate" style="max-width: 260px">{{ w.instructions.split('\n')[0] }}</div>
              </td>
              <td class="hide-mobile">
                <RouterLink v-if="auth.isAdmin" to="/integrations">{{ integrationName(w.integration_id) }}</RouterLink>
                <span v-else class="mono small">{{ w.integration_id }}</span>
              </td>
              <td class="hide-mobile">{{ target(w) }}</td>
              <td>
                <div class="row wrap" style="gap: 4px">
                  <span class="badge" :class="w.verify_deploys ? 'ok' : 'outline'" :title="w.verify_deploys ? 'Successful deploys are verified' : 'Deploys are not verified'">
                    <Icon :name="w.verify_deploys ? 'checkCircle' : 'pause'" />verify
                  </span>
                  <span class="badge" :class="w.triage_failures ? 'warn' : 'outline'" :title="w.triage_failures ? 'Failed deploys and crashed containers are triaged' : 'Failures are not triaged'">
                    <Icon :name="w.triage_failures ? 'siren' : 'pause'" />triage
                  </span>
                  <span v-if="w.databases" class="badge info" title="Failed backups, restores, provisioning and upgrades are triaged"><Icon name="hardDrive" />databases</span>
                </div>
              </td>
              <td class="hide-mobile"><span class="badge outline square" :title="AUTONOMY_LEVELS[w.autonomy]?.help">L{{ w.autonomy }}</span></td>
              <td class="nowrap hide-mobile" :title="fmtDate(w.last_event_at)">{{ w.last_event_at ? relTime(w.last_event_at, now) : 'never' }}</td>
              <td v-if="auth.isAdmin" class="right nowrap">
                <div class="row end" style="gap: 4px">
                  <button type="button" class="btn btn-sm btn-ghost btn-icon" :aria-label="`Edit the watch on ${watchLabel(w)}`" title="Edit" :disabled="busy === w.id" @click="openEdit(w)"><Icon name="edit" /></button>
                  <button type="button" class="btn btn-sm btn-ghost btn-icon btn-danger-ghost" :aria-label="`Stop watching ${watchLabel(w)}`" title="Delete" :disabled="busy === w.id" @click="remove(w)"><Icon name="trash" /></button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <Modal :open="show" :title="editing ? `Edit watch on ${watchLabel(editing)}` : 'Watch a Miabi app'" wide :dismissable="!saving" @close="show = false">
      <form id="watch-form" class="stack loose" novalidate @submit.prevent="save">
        <div v-if="formError" class="banner danger" role="alert"><Icon name="alert" /><div class="banner-body">{{ formError }}</div></div>
        <div class="grid-2">
          <div class="field">
            <label for="mw-int">Integration <span class="req" aria-hidden="true">*</span></label>
            <select id="mw-int" v-model="form.integration_id" class="select" :class="{ invalid: err('integration') }">
              <option value="" disabled>Select a Miabi integration</option>
              <option v-for="i in miabiIntegrations" :key="i.id" :value="i.id">{{ i.name }}{{ i.workspace ? ` · ${i.workspace}` : '' }}</option>
            </select>
            <span v-if="err('integration')" class="error-msg"><Icon name="alert" />{{ err('integration') }}</span>
            <span v-else-if="!miabiIntegrations.length" class="hint warn">No Miabi integration yet. <RouterLink to="/integrations" @click="show = false">Add one</RouterLink>.</span>
          </div>
          <div class="field">
            <label for="mw-ws">Workspace <span v-if="formWorkspaces.length > 1" class="req" aria-hidden="true">*</span></label>
            <select id="mw-ws" v-model="form.workspace" class="select mono" :class="{ invalid: err('workspace') }" :disabled="wsLoading || !form.integration_id">
              <option value="" :disabled="formWorkspaces.length > 1">{{ wsLoading ? 'Loading…' : formWorkspaces.length > 1 ? 'Select a workspace' : 'Default workspace' }}</option>
              <option v-if="form.workspace && !formWorkspaces.some((w) => w.name === form.workspace)" :value="form.workspace">{{ form.workspace }} (not enabled)</option>
              <option v-for="w in formWorkspaces" :key="w.id" :value="w.name">{{ w.name }}{{ w.display_name && w.display_name !== w.name ? ` · ${w.display_name}` : '' }}</option>
            </select>
            <span v-if="err('workspace')" class="error-msg"><Icon name="alert" />{{ err('workspace') }}</span>
            <span v-else-if="form.integration_id && !wsLoading && !formWorkspaces.length" class="hint warn">No enabled workspace. <RouterLink to="/integrations" @click="show = false">Enable one under Integrations → Workspaces</RouterLink>.</span>
            <span v-else class="hint">Only the integration's enabled workspaces are listed.</span>
          </div>
          <div class="field span-all">
            <label for="mw-app">App <span class="req" aria-hidden="true">*</span></label>
            <input id="mw-app" v-model="form.app" class="input mono" :class="{ invalid: err('app') }" maxlength="128" required spellcheck="false" autocomplete="off" placeholder="api" />
            <span v-if="err('app')" class="error-msg"><Icon name="alert" />{{ err('app') }}</span>
            <span v-else class="hint">app name, or <code>*</code> for every app, or a pattern like <code>api-*</code></span>
          </div>
        </div>

        <fieldset class="rule-block stack tight" style="margin: 0">
          <legend class="strong" style="padding: 0 4px">What to watch</legend>
          <label class="switch"><input v-model="form.verify_deploys" type="checkbox" />Verify successful deploys</label>
          <p class="xs muted" style="margin: 0 0 6px">After <code>deploy.succeeded</code>, an agent checks the app; if it is unhealthy it proposes a rollback change plan for a human to approve.</p>
          <label class="switch"><input v-model="form.triage_failures" type="checkbox" />Triage failures</label>
          <p class="xs muted" style="margin: 0 0 6px">On failed deploys, drift and crashed or OOM-killed containers, an agent investigates the logs and reports.</p>
          <label class="switch"><input v-model="form.databases" type="checkbox" />Databases</label>
          <p class="xs muted" style="margin: 0">Triage failed backups, restores, provisioning and upgrades of the workspace's databases.</p>
          <span v-if="nothingWatched" class="hint warn">All are off: events for this app are ignored.</span>
        </fieldset>

        <div class="field">
          <label for="mw-health">Health URL <span class="opt">(optional)</span></label>
          <input id="mw-health" v-model="form.health_url" class="input mono" :class="{ invalid: err('health_url') }" spellcheck="false" placeholder="https://api.example.com/healthz" />
          <span v-if="err('health_url')" class="error-msg"><Icon name="alert" />{{ err('health_url') }}</span>
          <span v-else class="hint">Checked during verification in addition to Miabi's own health status.</span>
        </div>

        <fieldset class="rule-block stack" style="margin: 0">
          <legend class="strong" style="padding: 0 4px">Where it runs</legend>
          <div class="choices" role="radiogroup" aria-label="Agent">
            <label class="choice" :class="{ on: form.target === 'agent' }">
              <input v-model="form.target" type="radio" name="mw-target" value="agent" /><Icon name="agents" />
              <span><span class="c-title">A specific agent</span><br /><span class="c-sub">e.g. an ops agent.</span></span>
            </label>
            <label class="choice" :class="{ on: form.target === 'labels' }">
              <input v-model="form.target" type="radio" name="mw-target" value="labels" /><Icon name="layers" />
              <span><span class="c-title">By labels</span><br /><span class="c-sub">Any agent with every label.</span></span>
            </label>
            <label class="choice" :class="{ on: form.target === 'any' }">
              <input v-model="form.target" type="radio" name="mw-target" value="any" /><Icon name="globe" />
              <span><span class="c-title">Any agent</span><br /><span class="c-sub">First available.</span></span>
            </label>
          </div>
          <div v-if="form.target === 'agent'" class="field">
            <label for="mw-agent">Agent</label>
            <select id="mw-agent" v-model="form.agent_id" class="select" :class="{ invalid: err('agent') }">
              <option value="" disabled>Select an agent</option>
              <option v-for="a in agents" :key="a.id" :value="a.id">{{ a.name }} · {{ a.status }}</option>
            </select>
            <span v-if="err('agent')" class="error-msg"><Icon name="alert" />{{ err('agent') }}</span>
          </div>
          <div v-else-if="form.target === 'labels'" class="field">
            <label for="mw-sel">Agent labels</label>
            <input id="mw-sel" v-model="form.selector" class="input mono" :class="{ invalid: err('labels') }" placeholder="ops, prod" />
            <span v-if="err('labels')" class="error-msg"><Icon name="alert" />{{ err('labels') }}</span>
            <span v-else class="hint">Comma separated.</span>
          </div>
          <span class="hint">The Miabi tools run on the control plane; the agent's policy still decides which workspaces and apps it may touch.</span>
        </fieldset>

        <div>
          <AutonomySelect id="mw-aut" v-model="form.autonomy" />
          <div class="banner info" style="margin-top: 8px">
            <Icon name="approvals" />
            <div class="banner-body">L2 lets the agent read status and logs on its own. Whatever the level, a <strong>rollback still needs an approved change plan</strong>.</div>
          </div>
        </div>

        <div class="field">
          <label for="mw-instr">Instructions <span class="opt">(optional)</span></label>
          <textarea id="mw-instr" v-model="form.instructions" class="textarea" rows="4" placeholder="What healthy looks like, known flaky checks, who to escalate to…" />
          <span class="hint">Added to every task from this watch. Miabi events are passed to the agent as data, never as instructions.</span>
        </div>
      </form>
      <template #footer>
        <button type="button" class="btn" @click="show = false">Cancel</button>
        <button type="submit" form="watch-form" class="btn btn-primary" :disabled="saving">
          <span v-if="saving" class="spinner" />{{ editing ? 'Save changes' : 'Watch app' }}
        </button>
      </template>
    </Modal>
  </div>
</template>

<style scoped>
.row-hl td {
  background: var(--bg-active);
}
</style>
