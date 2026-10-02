<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api, ApiError, type MCPServer, type MCPServerInput, type MCPTool, type MCPTransport, type Risk } from '../api'
import { useCoder } from '../stores/coder'
import { useConfirm } from '../stores/confirm'
import { useToast } from '../stores/toast'
import { fmtDate, relTime, splitList } from '../lib/format'
import { truncate } from '../lib/tools'
import { useNow } from '../lib/now'
import Badge from '../components/Badge.vue'
import Modal from '../components/Modal.vue'
import PageHeader from '../components/PageHeader.vue'
import EmptyState from '../components/EmptyState.vue'
import Icon from '../components/Icon'

const coder = useCoder()
const confirm = useConfirm()
const toast = useToast()
const now = useNow()

const servers = ref<MCPServer[]>([])
const loading = ref(true)
const busy = ref<string | null>(null)
const tools = ref<Record<string, MCPTool[]>>({})
const toolsLoading = ref<Record<string, boolean>>({})

const miabiIntegrations = computed(() => coder.integrations.filter((i) => i.kind === 'miabi'))
const integrationName = (id: string | null) => coder.integrations.find((i) => i.id === id)?.name ?? id ?? ''

async function load() {
  try {
    servers.value = (await api.listMCPServers()) ?? []
    for (const s of servers.value) loadTools(s.id)
  } catch {
    /* toasted */
  } finally {
    loading.value = false
  }
}

async function loadTools(id: string) {
  toolsLoading.value[id] = true
  try {
    setTools(id, (await api.listMCPTools(id, { quiet: true })) ?? [])
  } catch {
    setTools(id, [])
  } finally {
    toolsLoading.value[id] = false
  }
}

function setTools(id: string, list: MCPTool[]) {
  tools.value[id] = [...list].sort((a, b) => a.name.localeCompare(b.name))
  for (const t of list) drafts.value[t.id] = { enabled: t.enabled, risk: t.risk }
}

function replaceServer(s: MCPServer) {
  const i = servers.value.findIndex((x) => x.id === s.id)
  if (i >= 0) servers.value[i] = s
  else servers.value = [...servers.value, s].sort((a, b) => a.name.localeCompare(b.name))
}

/** The body that keeps a server as it is (env omitted: the stored values stay). */
function inputOf(s: MCPServer): MCPServerInput {
  return {
    name: s.name,
    transport: s.transport,
    command: s.command,
    args: s.args ?? [],
    url: s.url,
    integration_id: s.integration_id,
    allow_write: s.allow_write,
    enabled: s.enabled,
  }
}

async function toggle(s: MCPServer, ev: Event) {
  const box = ev.target as HTMLInputElement
  busy.value = s.id
  try {
    replaceServer(await api.updateMCPServer(s.id, { ...inputOf(s), enabled: !s.enabled }))
    toast.success(s.enabled ? `${s.name} disabled: its tools are no longer offered` : `${s.name} enabled`)
  } catch {
    box.checked = s.enabled
  } finally {
    busy.value = null
  }
}

async function sync(s: MCPServer) {
  busy.value = s.id
  try {
    const list = (await api.syncMCPServer(s.id)) ?? []
    setTools(s.id, list)
    toast.success(`${s.name}: ${list.length} tool${list.length === 1 ? '' : 's'}`)
  } catch {
    /* toasted */
  } finally {
    busy.value = null
    refreshServer(s.id)
  }
}

// Sync records last_error and synced_at on the server row.
async function refreshServer(id: string) {
  const list = await api.listMCPServers({ quiet: true }).catch(() => null)
  const s = list?.find((x) => x.id === id)
  if (s) replaceServer(s)
}

async function remove(s: MCPServer) {
  const ok = await confirm.ask({
    title: `Delete MCP server ${s.name}?`,
    message: `Its tools (mcp__${s.name}__*) stop being offered to agents and its stored environment is discarded.`,
    confirmText: `Delete ${s.name}`,
    danger: true,
  })
  if (!ok) return
  busy.value = s.id
  try {
    await api.deleteMCPServer(s.id)
    servers.value = servers.value.filter((x) => x.id !== s.id)
    delete tools.value[s.id]
    toast.success('MCP server deleted')
  } catch {
    /* toasted */
  } finally {
    busy.value = null
  }
}

interface Draft {
  enabled: boolean
  risk: '' | Risk
}
const drafts = ref<Record<string, Draft>>({})
const savingTool = ref<string | null>(null)
const toolError = ref<Record<string, string>>({})

const riskOptions = (t: MCPTool): Risk[] => (t.read_only ? ['low', 'medium', 'high', 'critical'] : ['medium', 'high', 'critical'])
const dirty = (t: MCPTool) => {
  const d = drafts.value[t.id]
  return !!d && (d.enabled !== t.enabled || d.risk !== t.risk)
}
function draftError(t: MCPTool): string {
  const d = drafts.value[t.id]
  if (!d) return ''
  if (d.enabled && !d.risk) return 'Pick a risk to enable it.'
  if (d.risk === 'low' && !t.read_only) return 'Low is only for read-only tools.'
  return ''
}

async function saveTool(s: MCPServer, t: MCPTool) {
  const d = drafts.value[t.id]
  toolError.value[t.id] = draftError(t)
  if (!d || toolError.value[t.id]) return
  savingTool.value = t.id
  try {
    const updated = await api.updateMCPTool(s.id, t.id, { enabled: d.enabled, risk: d.risk }, { quiet: true })
    const list = tools.value[s.id] ?? []
    const i = list.findIndex((x) => x.id === t.id)
    if (i >= 0) list[i] = updated
    drafts.value[t.id] = { enabled: updated.enabled, risk: updated.risk }
    toast.success(`${toolName(s, t)} ${updated.enabled ? `enabled · ${updated.risk} risk` : 'disabled'}`)
  } catch (e) {
    toolError.value[t.id] = e instanceof ApiError ? e.message : 'Saving failed.'
  } finally {
    savingTool.value = null
  }
}

const toolName = (s: MCPServer, t: MCPTool) => `mcp__${s.name}__${t.name}`
const enabledCount = (id: string) => (tools.value[id] ?? []).filter((t) => t.enabled).length

type Mode = 'miabi' | 'custom'
interface Pair {
  k: string
  v: string
}
interface Form {
  mode: Mode
  name: string
  integration_id: string
  allow_write: boolean
  transport: MCPTransport
  command: string
  args: string
  url: string
  env: Pair[]
  /** Edit only: send env (replacing the stored values) instead of keeping them. */
  replace_env: boolean
  enabled: boolean
}

const NAME_RE = /^[a-z0-9-]{1,32}$/

const show = ref(false)
const editing = ref<MCPServer | null>(null)
const saving = ref(false)
const tried = ref(false)
const formError = ref('')
const form = ref<Form>(blank())

function blank(): Form {
  return {
    mode: 'miabi',
    name: 'miabi',
    integration_id: miabiIntegrations.value[0]?.id ?? '',
    allow_write: false,
    transport: 'stdio',
    command: '',
    args: '',
    url: '',
    env: [{ k: '', v: '' }],
    replace_env: true,
    enabled: true,
  }
}

function setMode(m: Mode) {
  if (!editing.value) {
    if (m === 'custom' && form.value.name === 'miabi') form.value.name = ''
    if (m === 'miabi' && !form.value.name) form.value.name = 'miabi'
  }
  form.value.mode = m
}

function openNew() {
  editing.value = null
  form.value = blank()
  if (!miabiIntegrations.value.length) setMode('custom')
  tried.value = false
  formError.value = ''
  show.value = true
}

function openEdit(s: MCPServer) {
  editing.value = s
  form.value = {
    ...blank(),
    mode: s.integration_id ? 'miabi' : 'custom',
    name: s.name,
    integration_id: s.integration_id ?? '',
    allow_write: s.allow_write,
    transport: s.transport,
    command: s.command,
    args: (s.args ?? []).join('\n'),
    url: s.url,
    replace_env: false,
    enabled: s.enabled,
  }
  tried.value = false
  formError.value = ''
  show.value = true
}

const sendsEnv = computed(() => form.value.mode === 'custom' && (!editing.value || form.value.replace_env))
const dupKeys = computed(() => {
  const seen = new Set<string>()
  const dup = new Set<string>()
  for (const p of form.value.env) {
    const k = p.k.trim()
    if (!k) continue
    if (seen.has(k)) dup.add(k)
    seen.add(k)
  }
  return dup
})

const errors = computed(() => {
  const f = form.value
  const e: Record<string, string> = {}
  if (!NAME_RE.test(f.name.trim())) e.name = 'Lowercase letters, digits and -, at most 32 characters.'
  if (f.mode === 'miabi' && !f.integration_id) e.integration = 'Pick a Miabi integration.'
  if (f.mode === 'custom' && f.transport === 'stdio' && !f.command.trim()) e.command = 'Enter the command to run.'
  if (f.mode === 'custom' && f.transport === 'http' && !/^https?:\/\//i.test(f.url.trim())) e.url = 'Use a full http(s):// URL.'
  if (sendsEnv.value && dupKeys.value.size) e.env = 'Each key can appear once.'
  return e
})
const err = (k: string) => (tried.value ? errors.value[k] : '')

function inputFrom(f: Form): MCPServerInput {
  if (f.mode === 'miabi') return { name: f.name.trim(), integration_id: f.integration_id, allow_write: f.allow_write, enabled: f.enabled }
  const body: MCPServerInput = { name: f.name.trim(), transport: f.transport, integration_id: null, allow_write: false, enabled: f.enabled }
  if (f.transport === 'stdio') {
    body.command = f.command.trim()
    body.args = splitList(f.args, /\n/)
  } else {
    body.url = f.url.trim()
  }
  if (sendsEnv.value) {
    const env: Record<string, string> = {}
    for (const p of f.env) if (p.k.trim()) env[p.k.trim()] = p.v
    body.env = env
  } else {
    body.env = null
  }
  return body
}

async function save() {
  tried.value = true
  formError.value = ''
  if (Object.keys(errors.value).length) return
  saving.value = true
  try {
    const body = inputFrom(form.value)
    if (editing.value) {
      replaceServer(await api.updateMCPServer(editing.value.id, body, { quiet: true }))
      toast.success(`${body.name} saved`)
      loadTools(editing.value.id)
    } else {
      const res = await api.createMCPServer(body, { quiet: true })
      replaceServer(res.server)
      setTools(res.server.id, res.tools ?? [])
      if (res.error) toast.warn(`${res.server.name} added, but listing its tools failed: ${res.error}`)
      else toast.success(`${res.server.name} added with ${res.tools?.length ?? 0} tools, all disabled until you give them a risk`)
    }
    show.value = false
  } catch (e) {
    formError.value = e instanceof ApiError ? e.message : 'Saving failed.'
  } finally {
    saving.value = false
  }
}

onMounted(() => {
  load()
  coder.loadIntegrations()
})
</script>

<template>
  <div>
    <PageHeader title="MCP servers" subtitle="Model Context Protocol servers the control plane runs or reaches. Their tools are offered to agents as mcp__<server>__<tool>, once you enable them and give each a risk.">
      <button type="button" class="btn btn-primary" @click="openNew"><Icon name="plus" />Add MCP server</button>
    </PageHeader>

    <div class="banner info" style="margin-bottom: 16px">
      <Icon name="policies" />
      <div class="banner-body">
        Agents only get the MCP tools their policy allows <strong>by name</strong>. Templates that list tools explicitly need e.g. <code>mcp__miabi__*</code> added.
        Calls run on the control plane, never on the agent, and are audited like every other tool.
      </div>
    </div>

    <div v-if="loading" class="card card-body row small muted"><span class="spinner" />Loading MCP servers…</div>
    <div v-else-if="!servers.length" class="card">
      <EmptyState title="No MCP servers yet" icon="network">
        Add Miabi's MCP server from an integration, or any other stdio or HTTP MCP server. Its tools stay disabled until you enable them with a risk.
        <template #actions><button type="button" class="btn btn-primary" @click="openNew"><Icon name="plus" />Add MCP server</button></template>
      </EmptyState>
    </div>

    <div v-else class="stack loose">
      <section v-for="s in servers" :key="s.id" class="card" :aria-labelledby="`mcp-${s.id}`">
        <div class="card-head" style="flex-wrap: wrap">
          <h2 :id="`mcp-${s.id}`"><Icon :name="s.integration_id ? 'layers' : 'network'" />{{ s.name }}</h2>
          <span class="badge outline square">{{ s.transport }}</span>
          <span class="grow" />
          <label class="switch">
            <input type="checkbox" :checked="s.enabled" :disabled="busy === s.id" :aria-label="`Enable ${s.name}`" @change="toggle(s, $event)" />
            <span class="small">{{ s.enabled ? 'Enabled' : 'Disabled' }}</span>
          </label>
          <div class="row" style="gap: 4px">
            <button type="button" class="btn btn-sm" :disabled="busy === s.id" @click="sync(s)">
              <span v-if="busy === s.id" class="spinner" /><Icon v-else name="refresh" />Sync
            </button>
            <button type="button" class="btn btn-sm btn-ghost btn-icon" :aria-label="`Edit ${s.name}`" title="Edit" :disabled="busy === s.id" @click="openEdit(s)"><Icon name="edit" /></button>
            <button type="button" class="btn btn-sm btn-ghost btn-icon btn-danger-ghost" :aria-label="`Delete ${s.name}`" title="Delete" :disabled="busy === s.id" @click="remove(s)"><Icon name="trash" /></button>
          </div>
        </div>
        <div class="card-body stack tight">
          <dl class="kv">
            <template v-if="s.integration_id">
              <dt>Integration</dt>
              <dd><RouterLink to="/integrations">{{ integrationName(s.integration_id) }}</RouterLink> <span class="small muted">· runs <code>miabi mcp</code>{{ s.allow_write ? ' · write tools allowed' : ' · read-only' }}</span></dd>
            </template>
            <template v-else-if="s.transport === 'stdio'">
              <dt>Command</dt>
              <dd class="mono small" style="overflow-wrap: anywhere">{{ [s.command, ...(s.args ?? [])].join(' ') }}</dd>
            </template>
            <template v-else>
              <dt>URL</dt>
              <dd class="mono small" style="overflow-wrap: anywhere">{{ s.url }}</dd>
            </template>
            <template v-if="s.env_keys?.length">
              <dt>{{ s.transport === 'http' ? 'Headers' : 'Environment' }}</dt>
              <dd class="row wrap" style="gap: 4px"><span v-for="k in s.env_keys" :key="k" class="label-chip mono" title="Value is write-only">{{ k }}</span></dd>
            </template>
            <dt>Synced</dt>
            <dd :title="fmtDate(s.synced_at)">{{ s.synced_at ? relTime(s.synced_at, now) : 'never' }}</dd>
          </dl>
          <div v-if="s.last_error" class="small danger-text" style="overflow-wrap: anywhere"><Icon name="alert" :size="13" style="vertical-align: -2px" /> {{ s.last_error }}</div>
        </div>

        <div class="table-wrap">
          <table class="table">
            <caption class="sr-only">Tools of {{ s.name }}</caption>
            <thead>
              <tr>
                <th>Tool <span class="muted" style="font-weight: 400">{{ enabledCount(s.id) }}/{{ tools[s.id]?.length ?? 0 }} enabled</span></th>
                <th>Enabled</th><th>Risk</th><th><span class="sr-only">Save</span></th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="toolsLoading[s.id] && !tools[s.id]?.length"><td colspan="4" class="small muted"><span class="spinner" /> Loading tools…</td></tr>
              <tr v-else-if="!tools[s.id]?.length"><td colspan="4" class="small muted">No tools listed. Sync to read them from the server.</td></tr>
              <tr v-for="t in tools[s.id] ?? []" :key="t.id">
                <td style="max-width: 460px">
                  <div class="row wrap" style="gap: 6px">
                    <span class="cell-title">{{ t.name }}</span>
                    <span v-if="t.read_only" class="badge ok"><Icon name="eye" />read-only</span>
                    <span v-if="t.destructive" class="badge danger"><Icon name="alert" />destructive</span>
                  </div>
                  <div class="cell-sub mono">{{ toolName(s, t) }}</div>
                  <div v-if="t.description" class="cell-sub truncate" :title="t.description">{{ truncate(t.description, 140) }}</div>
                  <div v-if="toolError[t.id]" class="error-msg"><Icon name="alert" />{{ toolError[t.id] }}</div>
                </td>
                <td>
                  <label v-if="drafts[t.id]" class="switch">
                    <input v-model="drafts[t.id].enabled" type="checkbox" :aria-label="`Enable ${toolName(s, t)}`" @change="toolError[t.id] = ''" />
                    <span class="sr-only">{{ drafts[t.id].enabled ? 'enabled' : 'disabled' }}</span>
                  </label>
                </td>
                <td>
                  <select v-if="drafts[t.id]" v-model="drafts[t.id].risk" class="select sm" :aria-label="`Risk of ${toolName(s, t)}`" :class="{ invalid: drafts[t.id].enabled && !drafts[t.id].risk }" @change="toolError[t.id] = ''">
                    <option value="">No risk set</option>
                    <option v-for="r in riskOptions(t)" :key="r" :value="r">{{ r }}</option>
                    <option v-if="t.risk === 'low' && !t.read_only" value="low" disabled>low</option>
                  </select>
                </td>
                <td class="right nowrap">
                  <Badge v-if="!dirty(t) && t.enabled && t.risk" :value="t.risk" kind="risk" />
                  <button v-else type="button" class="btn btn-sm" :class="{ 'btn-primary': dirty(t) }" :disabled="!dirty(t) || savingTool === t.id" @click="saveTool(s, t)">
                    <span v-if="savingTool === t.id" class="spinner" /><Icon v-else name="check" />Save
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </div>

    <Modal :open="show" :title="editing ? `Edit ${editing.name}` : 'Add MCP server'" wide :dismissable="!saving" @close="show = false">
      <form id="mcp-form" class="stack loose" novalidate @submit.prevent="save">
        <div v-if="formError" class="banner danger" role="alert"><Icon name="alert" /><div class="banner-body">{{ formError }}</div></div>
        <div class="field">
          <span id="mcp-mode" class="label">Source</span>
          <div class="segmented" role="radiogroup" aria-labelledby="mcp-mode" style="align-self: flex-start">
            <button type="button" role="radio" :aria-checked="form.mode === 'miabi'" :class="{ on: form.mode === 'miabi' }" :disabled="!!editing" @click="setMode('miabi')"><Icon name="layers" />Miabi (from an integration)</button>
            <button type="button" role="radio" :aria-checked="form.mode === 'custom'" :class="{ on: form.mode === 'custom' }" :disabled="!!editing" @click="setMode('custom')"><Icon name="network" />Custom</button>
          </div>
        </div>

        <div class="grid-2">
          <div class="field">
            <label for="mcp-name">Name <span class="req" aria-hidden="true">*</span></label>
            <input id="mcp-name" v-model="form.name" class="input mono" :class="{ invalid: err('name') }" maxlength="32" required spellcheck="false" autocomplete="off" placeholder="miabi" />
            <span v-if="err('name')" class="error-msg"><Icon name="alert" />{{ err('name') }}</span>
            <span v-else class="hint">Tools become <code>mcp__{{ form.name.trim() || '&lt;name&gt;' }}__&lt;tool&gt;</code>.</span>
          </div>
          <label class="switch" style="align-self: end; padding-bottom: 8px"><input v-model="form.enabled" type="checkbox" />Enabled</label>
        </div>

        <template v-if="form.mode === 'miabi'">
          <div class="grid-2">
            <div class="field">
              <label for="mcp-int">Miabi integration <span class="req" aria-hidden="true">*</span></label>
              <select id="mcp-int" v-model="form.integration_id" class="select" :class="{ invalid: err('integration') }">
                <option value="" disabled>Select a Miabi integration</option>
                <option v-for="i in miabiIntegrations" :key="i.id" :value="i.id">{{ i.name }}</option>
              </select>
              <span v-if="err('integration')" class="error-msg"><Icon name="alert" />{{ err('integration') }}</span>
              <span v-else-if="!miabiIntegrations.length" class="hint warn">No Miabi integration yet. <RouterLink to="/integrations" @click="show = false">Add one</RouterLink>.</span>
            </div>
            <label class="check" style="align-self: end; padding-bottom: 8px">
              <input v-model="form.allow_write" type="checkbox" />
              <span>Allow write tools</span>
            </label>
          </div>
          <div class="banner info">
            <Icon name="info" />
            <div class="banner-body stack tight">
              <span>Runs <code>miabi mcp</code> on the control plane with the integration's URL and API key; the key never reaches an agent.</span>
              <span>Every tool starts disabled, and write tools stay disabled until you give them a risk.</span>
              <span>MCP tools <strong>cannot be scoped to workspaces or apps by policy</strong>, only allowed or denied by name. Prefer the native Miabi tools (<code>miabi_*</code>) for changes.</span>
            </div>
          </div>
        </template>

        <template v-else>
          <div class="field">
            <span id="mcp-tr" class="label">Transport</span>
            <div class="segmented" role="radiogroup" aria-labelledby="mcp-tr" style="align-self: flex-start">
              <button type="button" role="radio" :aria-checked="form.transport === 'stdio'" :class="{ on: form.transport === 'stdio' }" @click="form.transport = 'stdio'"><Icon name="terminal" />stdio</button>
              <button type="button" role="radio" :aria-checked="form.transport === 'http'" :class="{ on: form.transport === 'http' }" @click="form.transport = 'http'"><Icon name="globe" />HTTP</button>
            </div>
          </div>
          <div v-if="form.transport === 'stdio'" class="grid-2">
            <div class="field">
              <label for="mcp-cmd">Command <span class="req" aria-hidden="true">*</span></label>
              <input id="mcp-cmd" v-model="form.command" class="input mono" :class="{ invalid: err('command') }" spellcheck="false" autocomplete="off" placeholder="npx" />
              <span v-if="err('command')" class="error-msg"><Icon name="alert" />{{ err('command') }}</span>
              <span v-else class="hint">Must be allowed by <code>AKILI_MCP_COMMANDS</code> on the control plane.</span>
            </div>
            <div class="field">
              <label for="mcp-args">Arguments <span class="opt">(one per line)</span></label>
              <textarea id="mcp-args" v-model="form.args" class="textarea mono" rows="3" spellcheck="false" placeholder="-y&#10;@modelcontextprotocol/server-github" />
            </div>
          </div>
          <div v-else class="field">
            <label for="mcp-url">URL <span class="req" aria-hidden="true">*</span></label>
            <input id="mcp-url" v-model="form.url" class="input mono" :class="{ invalid: err('url') }" spellcheck="false" autocomplete="off" placeholder="https://mcp.example.com/mcp" />
            <span v-if="err('url')" class="error-msg"><Icon name="alert" />{{ err('url') }}</span>
          </div>

          <fieldset class="rule-block stack tight" style="margin: 0">
            <legend class="strong" style="padding: 0 4px">{{ form.transport === 'http' ? 'Headers' : 'Environment' }}</legend>
            <template v-if="editing && !form.replace_env">
              <div class="row wrap" style="gap: 4px">
                <span v-for="k in editing.env_keys ?? []" :key="k" class="label-chip mono">{{ k }}</span>
                <span v-if="!editing.env_keys?.length" class="small muted">none stored</span>
              </div>
              <div class="row">
                <button type="button" class="btn btn-sm" @click="form.replace_env = true"><Icon name="edit" />Replace values</button>
                <span class="hint">Values are write-only; the stored ones are kept.</span>
              </div>
            </template>
            <template v-else>
              <p class="xs muted" style="margin: 0">Write-only: stored encrypted on the control plane; only the names are shown again.{{ editing ? ' Saving replaces every stored value.' : '' }}</p>
              <div v-for="(p, i) in form.env" :key="i" class="row pair">
                <label :for="`mcp-k-${i}`" class="sr-only">Key {{ i + 1 }}</label>
                <input :id="`mcp-k-${i}`" v-model="p.k" class="input mono sm" :class="{ invalid: dupKeys.has(p.k.trim()) }" :placeholder="form.transport === 'http' ? 'Authorization' : 'GITHUB_TOKEN'" spellcheck="false" autocomplete="off" />
                <span class="muted" aria-hidden="true">=</span>
                <label :for="`mcp-v-${i}`" class="sr-only">Value {{ i + 1 }}</label>
                <input :id="`mcp-v-${i}`" v-model="p.v" class="input mono sm" type="password" autocomplete="new-password" spellcheck="false" />
                <button type="button" class="btn btn-ghost btn-icon btn-sm" :aria-label="`Remove ${p.k || `row ${i + 1}`}`" @click="form.env.splice(i, 1)"><Icon name="x" /></button>
              </div>
              <div class="row">
                <button type="button" class="btn btn-sm" @click="form.env.push({ k: '', v: '' })"><Icon name="plus" />Add {{ form.transport === 'http' ? 'header' : 'variable' }}</button>
                <span v-if="err('env')" class="error-msg"><Icon name="alert" />{{ err('env') }}</span>
              </div>
            </template>
          </fieldset>
          <p class="small muted" style="margin: 0">Every tool the server lists appears below after a sync. Read-only tools start enabled at low risk; any other tool stays disabled until you enable it with a risk.</p>
        </template>
      </form>
      <template #footer>
        <button type="button" class="btn" @click="show = false">Cancel</button>
        <button type="submit" form="mcp-form" class="btn btn-primary" :disabled="saving">
          <span v-if="saving" class="spinner" />{{ editing ? 'Save' : 'Add server' }}
        </button>
      </template>
    </Modal>
  </div>
</template>

<style scoped>
.pair .input {
  flex: 1;
  min-width: 0;
}
</style>
