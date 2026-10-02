<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api, ApiError, type ForgeAuth, type Integration, type IntegrationInput, type IntegrationKind, type MiabiWorkspace, type TestResult } from '../api'
import { useCoder } from '../stores/coder'
import { useConfirm } from '../stores/confirm'
import { useToast } from '../stores/toast'
import { fmtDate, randomHex, relTime } from '../lib/format'
import { useNow } from '../lib/now'
import Modal from '../components/Modal.vue'
import CopyField from '../components/CopyField.vue'
import PageHeader from '../components/PageHeader.vue'
import EmptyState from '../components/EmptyState.vue'
import SkeletonRows from '../components/SkeletonRows.vue'
import Icon, { type IconName } from '../components/Icon'

const coder = useCoder()
const confirm = useConfirm()
const toast = useToast()
const now = useNow()

const list = ref<Integration[]>([])
const loading = ref(true)
const tests = ref<Record<string, TestResult | 'running'>>({})

const show = ref(false)
const editing = ref<Integration | null>(null)
/** After creating: the saved integration, shown with its webhook URL and generated secret. */
const saved = ref<Integration | null>(null)
const savedSecret = ref('')
const saving = ref(false)
const tried = ref(false)
const formError = ref('')

const GITHUB_API = 'https://api.github.com'
const GITHUB_WEB = 'https://github.com'

const blank = (): IntegrationInput => ({
  name: '',
  kind: 'gitea',
  base_url: '',
  web_url: '',
  auth_type: 'token',
  username: '',
  token: '',
  app_id: 0,
  installation_id: 0,
  private_key: '',
  webhook_secret: '',
  workspace: '',
  ca_cert: '',
  sender: '',
})
const form = ref<IntegrationInput>(blank())
const generated = ref(false)

async function load() {
  try {
    list.value = (await api.listIntegrations()) ?? []
    coder.integrations = list.value
  } catch {
    /* toasted */
  } finally {
    loading.value = false
  }
}

function openNew() {
  editing.value = null
  saved.value = null
  form.value = blank()
  generated.value = false
  tried.value = false
  formError.value = ''
  show.value = true
}

function openEdit(it: Integration) {
  editing.value = it
  saved.value = null
  form.value = {
    ...blank(),
    name: it.name,
    kind: it.kind,
    base_url: it.base_url,
    web_url: it.web_url,
    auth_type: it.auth_type,
    username: it.username,
    app_id: it.app_id ?? 0,
    installation_id: it.installation_id ?? 0,
    workspace: it.workspace ?? '',
    ca_cert: it.ca_cert ?? '',
    sender: it.sender ?? '',
  }
  generated.value = false
  tried.value = false
  formError.value = ''
  show.value = true
}

const KINDS: { kind: IntegrationKind; label: string; icon: IconName }[] = [
  { kind: 'gitea', label: 'Gitea', icon: 'gitea' },
  { kind: 'github', label: 'GitHub', icon: 'github' },
  { kind: 'miabi', label: 'Miabi', icon: 'layers' },
  { kind: 'posta', label: 'Posta', icon: 'mail' },
]
// Kinds with a default: Miabi tool calls and Posta email use it when nothing names one.
const hasDefault = (k: IntegrationKind) => k === 'miabi' || k === 'posta'
const hasCA = hasDefault
const kindOf = (k: IntegrationKind) => KINDS.find((x) => x.kind === k) ?? KINDS[0]
const MIABI_EVENTS = ['deploy.succeeded', 'deploy.failed', 'container.died', 'container.oom']

function setKind(k: IntegrationKind) {
  form.value.kind = k
  if (k === 'github') {
    if (!form.value.base_url) form.value.base_url = GITHUB_API
    if (!form.value.web_url) form.value.web_url = GITHUB_WEB
  } else {
    form.value.auth_type = 'token'
    if (form.value.base_url === GITHUB_API) form.value.base_url = ''
    if (form.value.web_url === GITHUB_WEB) form.value.web_url = ''
  }
}
function setAuth(a: ForgeAuth) {
  form.value.auth_type = a
}

function generateSecret() {
  form.value.webhook_secret = randomHex(32)
  generated.value = true
}

// ---- validation (mirrors the server so mistakes show before the round trip) ----------------------
const hasStored = computed(() => !!editing.value?.has_secret && editing.value.auth_type === form.value.auth_type)
const errors = computed(() => {
  const f = form.value
  const e: Record<string, string> = {}
  if (!f.name.trim()) e.name = 'Give the integration a name.'
  if (f.kind === 'gitea' && !f.base_url.trim()) e.base_url = 'Enter the Gitea URL, e.g. https://gitea.example.com.'
  if (f.kind === 'miabi' && !f.base_url.trim()) e.base_url = 'Enter the Miabi URL, e.g. https://miabi.example.com.'
  if (f.kind === 'posta' && !f.base_url.trim()) e.base_url = 'Enter the Posta URL, e.g. https://posta.example.com.'
  if (f.kind === 'posta' && !/^[^\r\n]*@[^\r\n]+$/.test((f.sender ?? '').trim())) e.sender = 'Enter the From address, e.g. Akili <akili@example.com>.'
  if (f.base_url.trim() && !/^https?:\/\//i.test(f.base_url.trim())) e.base_url = 'Use a full http(s):// URL.'
  if (f.auth_type === 'token' && !f.token.trim() && !hasStored.value) e.token = 'A token is required.'
  if (f.auth_type === 'github_app') {
    if (!Number(f.app_id)) e.app_id = 'Required.'
    if (!Number(f.installation_id)) e.installation_id = 'Required.'
    if (!f.private_key.trim() && !hasStored.value) e.private_key = 'Paste the App private key (PEM).'
  }
  return e
})
const err = (k: string) => (tried.value ? errors.value[k] : '')

async function save() {
  tried.value = true
  formError.value = ''
  if (Object.keys(errors.value).length) return
  saving.value = true
  const f = form.value
  const body: IntegrationInput = {
    ...f,
    name: f.name.trim(),
    base_url: f.base_url.trim(),
    web_url: f.kind === 'github' ? f.web_url.trim() : '',
    username: f.kind === 'gitea' || f.kind === 'github' ? f.username.trim() : '',
    workspace: f.kind === 'miabi' ? f.workspace.trim() : '',
    ca_cert: hasCA(f.kind) ? f.ca_cert.trim() : '',
    sender: f.kind === 'posta' ? (f.sender ?? '').trim() : '',
    token: f.auth_type === 'token' ? f.token.trim() : '',
    app_id: f.auth_type === 'github_app' ? Number(f.app_id) || 0 : 0,
    installation_id: f.auth_type === 'github_app' ? Number(f.installation_id) || 0 : 0,
    private_key: f.auth_type === 'github_app' ? f.private_key : '',
    webhook_secret: f.webhook_secret.trim(),
  }
  try {
    if (editing.value) {
      await api.updateIntegration(editing.value.id, body)
      show.value = false
      toast.success(`Integration ${body.name} updated`)
    } else {
      const it = await api.createIntegration(body)
      saved.value = it
      savedSecret.value = body.webhook_secret
      toast.success(`Integration ${body.name} added`)
    }
    load()
  } catch (e) {
    formError.value = e instanceof ApiError ? e.message : 'Saving failed.'
  } finally {
    saving.value = false
  }
}

const countOf = (k: IntegrationKind) => list.value.filter((i) => i.kind === k).length

async function makeDefault(it: Integration) {
  try {
    await api.setDefaultIntegration(it.id)
    toast.success(`${it.name} is now the default ${kindOf(it.kind).label} integration`)
    load()
  } catch {
    /* toasted */
  }
}

async function test(it: Integration) {
  tests.value[it.id] = 'running'
  try {
    tests.value[it.id] = await api.testIntegration(it.id)
  } catch {
    delete tests.value[it.id]
  }
}

async function remove(it: Integration) {
  if (it.kind === 'miabi') return removeMiabi(it)
  await coder.loadProjects(true)
  const using = coder.projects.filter((p) => p.integration_id === it.id)
  if (using.length) {
    toast.error(`${it.name} is used by ${using.length} project${using.length > 1 ? 's' : ''} (${using.map((p) => p.name).join(', ')}). Delete those projects first.`)
    return
  }
  if (!(await confirm.ask({ title: `Delete integration ${it.name}?`, message: 'Its stored credentials are discarded. Repositories on the forge are not touched.', confirmText: `Delete ${it.name}`, danger: true }))) return
  try {
    await api.deleteIntegration(it.id, { quiet: true })
    toast.success('Integration deleted')
    load()
  } catch (e) {
    if (e instanceof ApiError && e.status === 409) toast.error(`${it.name} is still used by projects. Delete those projects first.`)
    else if (e instanceof ApiError) toast.error(e.message)
  }
}

async function removeMiabi(it: Integration) {
  const watches = ((await api.listMiabiWatches({ quiet: true }).catch(() => null)) ?? []).filter((w) => w.integration_id === it.id)
  const message = watches.length
    ? `Its credentials are discarded and ${watches.length} watched app${watches.length > 1 ? 's' : ''} (${watches.map((w) => (w.workspace ? `${w.workspace}/${w.app}` : w.app)).join(', ')}) stop${watches.length > 1 ? '' : 's'} being verified. Nothing changes on Miabi.`
    : 'Its stored credentials are discarded. Nothing changes on Miabi.'
  if (!(await confirm.ask({ title: `Delete integration ${it.name}?`, message, confirmText: `Delete ${it.name}`, danger: true }))) return
  try {
    await api.deleteIntegration(it.id, { quiet: true })
    toast.success('Integration deleted')
    load()
  } catch (e) {
    if (e instanceof ApiError && e.status === 409) toast.error(`${it.name} is still used by Miabi watches. Delete those watches first.`)
    else if (e instanceof ApiError) toast.error(e.message)
  }
}

const wsFor = ref<Integration | null>(null)
const workspaces = ref<MiabiWorkspace[]>([])
const wsLoading = ref(false)
const wsSyncing = ref(false)
const wsBusy = ref<string | null>(null)

async function openWorkspaces(it: Integration) {
  wsFor.value = it
  workspaces.value = []
  wsLoading.value = true
  try {
    workspaces.value = (await api.listMiabiWorkspaces(it.id)) ?? []
  } catch {
    /* toasted */
  } finally {
    wsLoading.value = false
  }
}

function chooseWorkspaces() {
  if (!saved.value) return
  show.value = false
  openWorkspaces(saved.value)
}

async function syncWorkspaces() {
  const it = wsFor.value
  if (!it) return
  wsSyncing.value = true
  try {
    workspaces.value = (await api.syncMiabiWorkspaces(it.id)) ?? []
    toast.success(`${workspaces.value.length} workspace${workspaces.value.length === 1 ? '' : 's'} synced from Miabi`)
  } catch {
    /* toasted */
  } finally {
    wsSyncing.value = false
  }
}

async function toggleWorkspace(w: MiabiWorkspace, ev: Event) {
  const it = wsFor.value
  if (!it) return
  const box = ev.target as HTMLInputElement
  wsBusy.value = w.id
  try {
    const updated = await api.updateMiabiWorkspace(it.id, w.id, !w.enabled)
    const i = workspaces.value.findIndex((x) => x.id === w.id)
    if (i >= 0) workspaces.value[i] = updated
    toast.success(updated.enabled ? `Agents can use ${updated.name}` : `${updated.name} disabled for agents`)
  } catch {
    box.checked = w.enabled
  } finally {
    wsBusy.value = null
  }
}

const testOf = (id: string) => tests.value[id]
const done = (id: string): TestResult | null => {
  const t = tests.value[id]
  return t && t !== 'running' ? t : null
}
const dialogTitle = computed(() => (saved.value ? 'Integration added' : editing.value ? `Edit ${editing.value.name}` : 'Add integration'))
const webhookFor = computed(() => saved.value ?? editing.value)

onMounted(load)
</script>

<template>
  <div>
    <PageHeader title="Integrations" subtitle="Connections to git forges, Miabi and Posta (email). Credentials stay on the control plane: agents never see them, git traffic, pull requests, deploys and email go through it.">
      <button type="button" class="btn btn-primary" @click="openNew"><Icon name="plus" />Add integration</button>
    </PageHeader>

    <div class="card">
      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr><th>Name</th><th>Kind</th><th class="hide-mobile">Auth</th><th class="hide-mobile">Account</th><th>Secret</th><th>Connection</th><th><span class="sr-only">Actions</span></th></tr>
          </thead>
          <tbody>
            <SkeletonRows v-if="loading" :cols="7" :rows="2" />
            <tr v-else-if="!list.length">
              <td colspan="7">
                <EmptyState title="No integrations yet" icon="plug">
                  Connect Gitea or GitHub so agents can work on repositories (branches, commits and pull requests), Miabi so they can verify deploys and roll back, or Posta to send notification email. The credentials stay here.
                  <template #actions><button type="button" class="btn btn-primary" @click="openNew"><Icon name="plus" />Add integration</button></template>
                </EmptyState>
              </td>
            </tr>
            <tr v-for="it in loading ? [] : list" :key="it.id">
              <td>
                <span class="cell-title">{{ it.name }}</span>
                <span v-if="hasDefault(it.kind) && it.default && countOf(it.kind) > 1" class="badge ok" style="margin-left: 6px" :title="it.kind === 'posta' ? 'Sends notification email' : 'Used by tool calls that name no integration'">default</span>
                <div class="cell-sub mono truncate" style="max-width: 280px">{{ it.base_url }}</div>
              </td>
              <td class="nowrap">
                <span class="row" style="gap: 6px"><Icon :name="kindOf(it.kind).icon" :size="16" />{{ kindOf(it.kind).label }}</span>
              </td>
              <td class="nowrap hide-mobile">
                {{ it.auth_type === 'github_app' ? 'GitHub App' : 'Token' }}
                <div v-if="it.auth_type === 'github_app'" class="cell-sub mono">app {{ it.app_id }} · inst. {{ it.installation_id }}</div>
              </td>
              <td class="mono small hide-mobile">
                <template v-if="it.kind === 'miabi'"><template v-if="it.workspace"><span class="muted">default</span> {{ it.workspace }}</template><span v-else class="muted" title="Every workspace the key reaches; Test shows whether it is bound to one">any workspace</span></template>
                <template v-else-if="it.kind === 'posta'">{{ it.sender }}</template>
                <template v-else>{{ it.username || '—' }}</template>
              </td>
              <td>
                <span class="badge" :class="it.has_secret ? 'ok' : 'warn'"><Icon :name="it.has_secret ? 'lock' : 'alert'" />{{ it.has_secret ? 'set' : 'missing' }}</span>
              </td>
              <td style="max-width: 280px">
                <span v-if="testOf(it.id) === 'running'" class="row small muted"><span class="spinner" />testing…</span>
                <template v-else-if="done(it.id)">
                  <span class="badge" :class="done(it.id)!.ok ? 'ok' : 'danger'"><Icon :name="done(it.id)!.ok ? 'checkCircle' : 'xCircle'" />{{ done(it.id)!.ok ? `ok · ${done(it.id)!.latency_ms} ms` : 'failed' }}</span>
                  <div class="cell-sub" :class="{ 'danger-text': !done(it.id)!.ok }" style="white-space: normal; overflow-wrap: anywhere">{{ done(it.id)!.ok ? done(it.id)!.reply : done(it.id)!.error }}</div>
                </template>
                <span v-else class="small muted">not tested</span>
              </td>
              <td class="right nowrap">
                <div class="row end" style="gap: 4px">
                  <button v-if="hasDefault(it.kind) && !it.default && countOf(it.kind) > 1" type="button" class="btn btn-sm" :title="it.kind === 'posta' ? 'Send notification email through it' : 'Use it when a tool call names no integration'" @click="makeDefault(it)"><Icon name="award" />Make default</button>
                  <button v-if="it.kind === 'miabi'" type="button" class="btn btn-sm" @click="openWorkspaces(it)"><Icon name="folder" />Workspaces</button>
                  <button type="button" class="btn btn-sm" :disabled="testOf(it.id) === 'running'" @click="test(it)"><Icon name="zap" />Test</button>
                  <button type="button" class="btn btn-sm btn-ghost btn-icon" :aria-label="`Edit ${it.name}`" title="Edit" @click="openEdit(it)"><Icon name="edit" /></button>
                  <button type="button" class="btn btn-sm btn-ghost btn-icon btn-danger-ghost" :aria-label="`Delete ${it.name}`" title="Delete" @click="remove(it)"><Icon name="trash" /></button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <Modal :open="show" :title="dialogTitle" wide :dismissable="!saving" @close="show = false">
      <!-- created: webhook details -->
      <div v-if="saved" class="stack">
        <div class="banner ok"><Icon name="checkCircle" /><div class="banner-body"><strong>{{ saved.name }}</strong> is connected. Test it, then {{ saved.kind === 'miabi' ? 'enable its workspaces and watch apps from the Miabi page' : saved.kind === 'posta' ? 'send yourself a test email from Settings → Account, where each person chooses their email' : 'connect a repository from Projects' }}.</div></div>
        <div class="row">
          <button type="button" class="btn" :disabled="testOf(saved.id) === 'running'" @click="test(saved)">
            <span v-if="testOf(saved.id) === 'running'" class="spinner" /><Icon v-else name="zap" />Test connection
          </button>
          <span v-if="done(saved.id)" class="small" :class="done(saved.id)!.ok ? 'ok-text' : 'danger-text'">
            <Icon :name="done(saved.id)!.ok ? 'checkCircle' : 'xCircle'" :size="14" style="vertical-align: -2px" />
            {{ done(saved.id)!.ok ? done(saved.id)!.reply : done(saved.id)!.error }}
          </span>
        </div>
        <button v-if="saved.kind === 'miabi'" type="button" class="btn" style="align-self: flex-start" @click="chooseWorkspaces"><Icon name="folder" />Choose workspaces</button>
        <template v-if="saved.kind !== 'posta'">
        <div class="section-title" style="margin-top: 6px">{{ saved.kind === 'miabi' ? 'Outbound webhook (optional)' : 'Issue webhook (optional)' }}</div>
        <CopyField :value="saved.webhook_url" label="Webhook URL" />
        <template v-if="savedSecret">
          <CopyField :value="savedSecret" label="Webhook secret" />
          <div class="banner warn"><Icon name="alert" /><div class="banner-body">Copy the secret now: it is stored encrypted and never shown again.</div></div>
        </template>
        <p v-if="saved.kind === 'miabi'" class="small muted" style="margin: 0">
          Events of enabled workspaces with a watch are followed live, so a webhook is not needed. To use one anyway, add an <strong>outbound webhook</strong> in Miabi with this URL{{ savedSecret ? ' and secret' : '' }}, subscribed to
          <template v-for="(ev, i) in MIABI_EVENTS" :key="ev"><code>{{ ev }}</code>{{ i < MIABI_EVENTS.length - 1 ? ', ' : '' }}</template>.
          {{ savedSecret ? '' : 'If Miabi generates the signing secret, edit this integration and paste it. ' }}Then watch an app on the Miabi page.
        </p>
        <p v-else class="small muted" style="margin: 0">
          In the repository settings, add a webhook for <strong>Issue</strong> events with this URL{{ savedSecret ? ' and secret' : '' }} (content type JSON). Then label an issue with the project's trigger label and Akili turns it into a coding task.
        </p>
        </template>
      </div>

      <form v-else id="integration-form" class="stack" novalidate @submit.prevent="save">
        <div v-if="formError" class="banner danger" role="alert"><Icon name="alert" /><div class="banner-body">{{ formError }}</div></div>
        <div class="field">
          <span id="it-kind" class="label">Kind</span>
          <div class="segmented" role="radiogroup" aria-labelledby="it-kind" style="align-self: flex-start">
            <button v-for="k in KINDS" :key="k.kind" type="button" role="radio" :aria-checked="form.kind === k.kind" :class="{ on: form.kind === k.kind }" :disabled="!!editing" @click="setKind(k.kind)"><Icon :name="k.icon" />{{ k.label }}</button>
          </div>
          <span v-if="editing" class="hint">The kind of an existing integration cannot change; add a new one instead.</span>
          <span v-else-if="form.kind === 'miabi'" class="hint">Miabi runs your apps. Agents can read their status and logs, and deploy, roll back or restart them under policy.</span>
          <span v-else-if="form.kind === 'posta'" class="hint">Posta sends notification email: approval requests and finished tasks. Emails say what happened and link to Akili; tool arguments and output are never included.</span>
        </div>
        <div class="grid-2">
          <div class="field">
            <label for="it-name">Name <span class="req">*</span></label>
            <input id="it-name" v-model="form.name" class="input" :class="{ invalid: err('name') }" required maxlength="120" :placeholder="form.kind" />
            <span v-if="err('name')" class="error-msg"><Icon name="alert" />{{ err('name') }}</span>
          </div>
          <div v-if="form.kind === 'gitea'" class="field">
            <label for="it-url">Gitea URL <span class="req">*</span></label>
            <input id="it-url" v-model="form.base_url" class="input mono" :class="{ invalid: err('base_url') }" required placeholder="https://gitea.example.com" />
            <span v-if="err('base_url')" class="error-msg"><Icon name="alert" />{{ err('base_url') }}</span>
          </div>
          <template v-if="form.kind === 'miabi'">
            <div class="field">
              <label for="it-url">Miabi URL <span class="req">*</span></label>
              <input id="it-url" v-model="form.base_url" class="input mono" :class="{ invalid: err('base_url') }" required placeholder="https://miabi.example.com" />
              <span v-if="err('base_url')" class="error-msg"><Icon name="alert" />{{ err('base_url') }}</span>
            </div>
            <div class="field">
              <label for="it-ws">Default workspace <span class="opt">(optional)</span></label>
              <input id="it-ws" v-model="form.workspace" class="input mono" autocomplete="off" spellcheck="false" placeholder="acme" />
              <span class="hint">Id, uid or handle. Leave empty for an account-wide key; pick which workspaces agents may use under Workspaces after saving.</span>
            </div>
            <div class="field span-all">
              <label for="it-ca">CA certificate <span class="opt">(optional)</span></label>
              <textarea id="it-ca" v-model="form.ca_cert" class="input mono" rows="4" spellcheck="false" placeholder="-----BEGIN CERTIFICATE-----"></textarea>
              <span class="hint">Only when Miabi uses a self-signed or private-CA certificate: paste the CA (PEM). It is trusted for this integration only (API calls, event streams and the Miabi MCP server).</span>
              <span v-if="form.ca_cert && !form.ca_cert.includes('BEGIN CERTIFICATE')" class="error-msg"><Icon name="alert" />Paste a PEM certificate (-----BEGIN CERTIFICATE-----).</span>
            </div>
          </template>
          <template v-if="form.kind === 'posta'">
            <div class="field">
              <label for="it-url">Posta URL <span class="req">*</span></label>
              <input id="it-url" v-model="form.base_url" class="input mono" :class="{ invalid: err('base_url') }" required placeholder="https://posta.example.com" />
              <span v-if="err('base_url')" class="error-msg"><Icon name="alert" />{{ err('base_url') }}</span>
            </div>
            <div class="field">
              <label for="it-sender">From address <span class="req">*</span></label>
              <input id="it-sender" v-model="form.sender" class="input" :class="{ invalid: err('sender') }" required maxlength="320" placeholder="Akili <akili@example.com>" />
              <span v-if="err('sender')" class="error-msg"><Icon name="alert" />{{ err('sender') }}</span>
              <span v-else class="hint">On a domain verified in Posta.</span>
            </div>
            <div class="field span-all">
              <label for="it-ca">CA certificate <span class="opt">(optional)</span></label>
              <textarea id="it-ca" v-model="form.ca_cert" class="input mono" rows="4" spellcheck="false" placeholder="-----BEGIN CERTIFICATE-----"></textarea>
              <span class="hint">Only when Posta uses a self-signed or private-CA certificate: paste the CA (PEM). It is trusted for this integration only.</span>
              <span v-if="form.ca_cert && !form.ca_cert.includes('BEGIN CERTIFICATE')" class="error-msg"><Icon name="alert" />Paste a PEM certificate (-----BEGIN CERTIFICATE-----).</span>
            </div>
          </template>
          <div v-if="form.kind === 'gitea'" class="field">
            <label for="it-user">Username</label>
            <input id="it-user" v-model="form.username" class="input mono" autocomplete="off" placeholder="akili-bot" />
            <span class="hint">The token's owner. New repositories default to this owner.</span>
          </div>
          <template v-if="form.kind === 'github'">
            <div class="field span-all">
              <span id="it-auth" class="label">Authentication</span>
              <div class="segmented" role="radiogroup" aria-labelledby="it-auth" style="align-self: flex-start">
                <button type="button" role="radio" :aria-checked="form.auth_type === 'token'" :class="{ on: form.auth_type === 'token' }" @click="setAuth('token')"><Icon name="key" />Token</button>
                <button type="button" role="radio" :aria-checked="form.auth_type === 'github_app'" :class="{ on: form.auth_type === 'github_app' }" @click="setAuth('github_app')"><Icon name="github" />GitHub App</button>
              </div>
            </div>
            <div class="field">
              <label for="it-api">API URL</label>
              <input id="it-api" v-model="form.base_url" class="input mono" :class="{ invalid: err('base_url') }" :placeholder="GITHUB_API" />
              <span v-if="err('base_url')" class="error-msg"><Icon name="alert" />{{ err('base_url') }}</span>
              <span v-else class="hint">Change for GitHub Enterprise.</span>
            </div>
            <div class="field">
              <label for="it-web">Web URL</label>
              <input id="it-web" v-model="form.web_url" class="input mono" :placeholder="GITHUB_WEB" />
            </div>
            <div v-if="form.auth_type === 'token'" class="field">
              <label for="it-user">Username</label>
              <input id="it-user" v-model="form.username" class="input mono" autocomplete="off" placeholder="the token's user or org" />
              <span class="hint">New repositories default to this owner.</span>
            </div>
          </template>

          <div v-if="form.auth_type === 'token'" class="field span-all">
            <label for="it-token">{{ form.kind === 'miabi' || form.kind === 'posta' ? 'API key' : 'Access token' }} <span v-if="!hasStored" class="req">*</span></label>
            <input
              id="it-token"
              v-model="form.token"
              class="input mono"
              :class="{ invalid: err('token') }"
              type="password"
              autocomplete="new-password"
              :placeholder="hasStored ? '•••• set — leave empty to keep' : form.kind === 'miabi' ? 'mb_…' : form.kind === 'posta' ? 'psk_…' : ''"
            />
            <span v-if="err('token')" class="error-msg"><Icon name="alert" />{{ err('token') }}</span>
            <span v-else class="hint">Write-only.
              <template v-if="form.kind === 'miabi'">A Miabi <code>mb_</code> API key with the <strong>write</strong> and <strong>deploy</strong> scopes.</template>
              <template v-else-if="form.kind === 'posta'">A Posta API key bound to one workspace, allowed to send email.</template>
              <template v-else-if="form.kind === 'gitea'">Needs repository and issue read/write (and organization, to create repos in one).</template>
              <template v-else>A fine-grained token with Contents, Pull requests, Issues and Commit statuses (read/write), or a classic token with <code>repo</code>.</template>
            </span>
          </div>
          <template v-else>
            <div class="field">
              <label for="it-app">App ID <span class="req">*</span></label>
              <input id="it-app" v-model.number="form.app_id" class="input mono" :class="{ invalid: err('app_id') }" type="number" min="1" inputmode="numeric" />
              <span v-if="err('app_id')" class="error-msg"><Icon name="alert" />{{ err('app_id') }}</span>
            </div>
            <div class="field">
              <label for="it-inst">Installation ID <span class="req">*</span></label>
              <input id="it-inst" v-model.number="form.installation_id" class="input mono" :class="{ invalid: err('installation_id') }" type="number" min="1" inputmode="numeric" />
              <span v-if="err('installation_id')" class="error-msg"><Icon name="alert" />{{ err('installation_id') }}</span>
            </div>
            <div class="field span-all">
              <label for="it-pem">Private key (PEM) <span v-if="!hasStored" class="req">*</span></label>
              <textarea
                id="it-pem"
                v-model="form.private_key"
                class="textarea mono"
                :class="{ invalid: err('private_key') }"
                rows="5"
                spellcheck="false"
                autocomplete="off"
                :placeholder="hasStored ? '•••• set — leave empty to keep' : '-----BEGIN RSA PRIVATE KEY-----\n…\n-----END RSA PRIVATE KEY-----'"
              />
              <span v-if="err('private_key')" class="error-msg"><Icon name="alert" />{{ err('private_key') }}</span>
              <span v-else class="hint">Write-only. Akili mints short-lived installation tokens from it.</span>
            </div>
          </template>

          <div v-if="form.kind !== 'posta'" class="field span-all">
            <label for="it-hook">Webhook secret <span class="opt">(optional)</span></label>
            <div class="row" style="gap: 8px">
              <input
                id="it-hook"
                v-model="form.webhook_secret"
                class="input mono grow"
                :type="generated ? 'text' : 'password'"
                autocomplete="new-password"
                :placeholder="editing ? 'Leave empty to keep the current secret' : form.kind === 'miabi' ? 'Verifies Miabi webhooks' : 'Verifies issue webhooks'"
                @input="generated = false"
              />
              <button type="button" class="btn" @click="generateSecret"><Icon name="refresh" />Generate</button>
            </div>
            <CopyField v-if="generated && form.webhook_secret" :value="form.webhook_secret" />
            <span v-if="generated" class="hint">Copy it now and paste it into the {{ form.kind === 'miabi' ? 'Miabi' : 'forge' }} webhook: it is not shown again after saving.</span>
            <span v-else-if="form.kind === 'miabi'" class="hint">Only for an outbound webhook: events of enabled, watched workspaces are already followed live. Paste the signing secret Miabi shows for the webhook, or generate one here.</span>
            <span v-else class="hint">Needed to accept issue webhooks (labelled issues become tasks).</span>
          </div>
        </div>

        <template v-if="editing && webhookFor && webhookFor.kind !== 'posta'">
          <div class="section-title" style="margin-top: 4px">{{ webhookFor.kind === 'miabi' ? 'Outbound webhook (optional)' : 'Issue webhook' }}</div>
          <CopyField :value="webhookFor.webhook_url" label="Webhook URL" />
          <p v-if="webhookFor.kind === 'miabi'" class="small muted" style="margin: 0">
            Not needed for live events. To use one anyway, add an <strong>outbound webhook</strong> in Miabi with this URL and the secret, subscribed to
            <template v-for="(ev, i) in MIABI_EVENTS" :key="ev"><code>{{ ev }}</code>{{ i < MIABI_EVENTS.length - 1 ? ', ' : '' }}</template>.
          </p>
          <p v-else class="small muted" style="margin: 0">Add a webhook for <strong>Issue</strong> events with this URL and the secret; label issues with the project's trigger label to turn them into tasks.</p>
        </template>
      </form>

      <template #footer>
        <template v-if="saved">
          <button type="button" class="btn btn-primary" @click="show = false">Done</button>
        </template>
        <template v-else>
          <div v-if="editing" class="row" style="margin-right: auto; gap: 10px; min-width: 0">
            <button type="button" class="btn" :disabled="testOf(editing.id) === 'running'" @click="test(editing)">
              <span v-if="testOf(editing.id) === 'running'" class="spinner" /><Icon v-else name="zap" />Test connection
            </button>
            <span v-if="done(editing.id)" class="small truncate" :class="done(editing.id)!.ok ? 'ok-text' : 'danger-text'" :title="done(editing.id)!.error">
              {{ done(editing.id)!.ok ? done(editing.id)!.reply : done(editing.id)!.error }}
            </span>
          </div>
          <button type="button" class="btn" @click="show = false">Cancel</button>
          <button type="submit" form="integration-form" class="btn btn-primary" :disabled="saving">
            <span v-if="saving" class="spinner" />{{ editing ? 'Save' : 'Add integration' }}
          </button>
        </template>
      </template>
    </Modal>
    <Modal :open="!!wsFor" :title="wsFor ? `Workspaces of ${wsFor.name}` : 'Workspaces'" wide @close="wsFor = null">
      <div class="stack">
        <div class="banner info">
          <Icon name="info" />
          <div class="banner-body">
            Agents can use only <strong>enabled</strong> workspaces, by their handle. Events of enabled workspaces that have a watch on the
            <RouterLink to="/miabi" @click="wsFor = null">Miabi page</RouterLink> are followed live: no webhook needed (it stays optional).
          </div>
        </div>
        <div class="row between wrap">
          <span class="small muted">{{ wsLoading ? 'Loading…' : `${workspaces.length} workspace${workspaces.length === 1 ? '' : 's'} visible to the key` }}</span>
          <button type="button" class="btn btn-sm" :disabled="wsSyncing || wsLoading" @click="syncWorkspaces">
            <span v-if="wsSyncing" class="spinner" /><Icon v-else name="refresh" />Sync workspaces
          </button>
        </div>
        <div class="table-wrap">
          <table class="table">
            <thead>
              <tr><th>Workspace</th><th>Enabled</th><th class="hide-mobile">Role</th><th>Events</th></tr>
            </thead>
            <tbody>
              <SkeletonRows v-if="wsLoading" :cols="4" :rows="2" />
              <tr v-else-if="!workspaces.length">
                <td colspan="4">
                  <EmptyState title="No workspaces yet" icon="folder">Sync to read the workspaces this key can see.</EmptyState>
                </td>
              </tr>
              <tr v-for="w in wsLoading ? [] : workspaces" :key="w.id">
                <td>
                  <span class="cell-title">{{ w.display_name || w.name }}</span>
                  <div class="cell-sub row wrap" style="gap: 6px">
                    <span class="mono">{{ w.name }}</span>
                    <span v-if="!w.accessible" class="badge warn" title="The API key is bound to another workspace"><Icon name="lock" />not accessible</span>
                  </div>
                  <div v-if="w.last_error" class="small danger-text" style="overflow-wrap: anywhere; white-space: normal"><Icon name="alert" :size="13" style="vertical-align: -2px" /> {{ w.last_error }}</div>
                </td>
                <td>
                  <label class="switch">
                    <input type="checkbox" :checked="w.enabled" :disabled="wsBusy === w.id || (!w.accessible && !w.enabled)" :aria-label="`Enable ${w.name}`" @change="toggleWorkspace(w, $event)" />
                    <span class="sr-only">{{ w.enabled ? 'enabled' : 'disabled' }}</span>
                  </label>
                </td>
                <td class="hide-mobile small">{{ w.role || '—' }}</td>
                <td class="nowrap">
                  <span v-if="w.streaming" class="badge ok" title="A live event stream is open"><Icon name="activity" />live</span>
                  <div class="cell-sub" :title="fmtDate(w.last_event_at)">{{ w.last_event_at ? `last event ${relTime(w.last_event_at, now)}` : 'no events yet' }}</div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <p v-if="workspaces.some((w) => w.synced_at)" class="xs muted" style="margin: 0">
          Synced {{ relTime(workspaces.map((w) => w.synced_at ?? '').sort().pop(), now) }}.
        </p>
      </div>
      <template #footer>
        <button type="button" class="btn btn-primary" @click="wsFor = null">Done</button>
      </template>
    </Modal>
  </div>
</template>
