<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { api, AUTONOMY_LEVELS, type AlertRoute, type AlertRouteCreated, type AlertRouteInput, type Autonomy } from '../api'
import { useCatalog } from '../stores/catalog'
import { useConfirm } from '../stores/confirm'
import { useToast } from '../stores/toast'
import { fmtDate, relTime, splitList } from '../lib/format'
import { copyText } from '../lib/clipboard'
import { useNow } from '../lib/now'
import Badge from '../components/Badge.vue'
import Modal from '../components/Modal.vue'
import CopyField from '../components/CopyField.vue'
import AutonomySelect from '../components/AutonomySelect.vue'
import PageHeader from '../components/PageHeader.vue'
import EmptyState from '../components/EmptyState.vue'
import SkeletonRows from '../components/SkeletonRows.vue'
import Icon from '../components/Icon'

const catalog = useCatalog()
const confirm = useConfirm()
const toast = useToast()
const route = useRoute()
const now = useNow()

const routes = ref<AlertRoute[]>([])
const loading = ref(true)
const busy = ref<string | null>(null)
/** ?route=<id> (from a task's alert chip) highlights that row. */
const highlight = computed(() => (typeof route.query.route === 'string' ? route.query.route : ''))

async function load() {
  try {
    routes.value = (await api.listAlertRoutes()) ?? []
  } catch {
    /* toasted */
  } finally {
    loading.value = false
  }
}

// ---- form -----------------------------------------------------------------------------------------

type Fallback = 'agent' | 'labels' | 'any'
interface Pair {
  k: string
  v: string
}
interface Form {
  name: string
  enabled: boolean
  match: Pair[]
  host_label: string
  fallback: Fallback
  agent_id: string
  selector: string
  autonomy: Autonomy
  instructions: string
}

const show = ref(false)
const editing = ref<AlertRoute | null>(null)
const saving = ref(false)
const tried = ref(false)
const form = ref<Form>(blank())

function blank(): Form {
  return { name: '', enabled: true, match: [{ k: 'severity', v: 'critical' }], host_label: 'instance', fallback: 'any', agent_id: '', selector: '', autonomy: 2, instructions: '' }
}

function formFrom(r: AlertRoute): Form {
  const match = Object.entries(r.match ?? {}).map(([k, v]) => ({ k, v }))
  return {
    name: r.name,
    enabled: r.enabled,
    match: match.length ? match : [{ k: '', v: '' }],
    host_label: r.host_label || 'instance',
    fallback: r.agent_id ? 'agent' : r.selector?.length ? 'labels' : 'any',
    agent_id: r.agent_id ?? '',
    selector: (r.selector ?? []).join(', '),
    autonomy: r.autonomy,
    instructions: r.instructions,
  }
}

function inputFrom(f: Form): AlertRouteInput {
  const match: Record<string, string> = {}
  for (const p of f.match) if (p.k.trim()) match[p.k.trim()] = p.v.trim()
  return {
    name: f.name.trim(),
    enabled: f.enabled,
    match,
    host_label: f.host_label.trim() === 'instance' ? '' : f.host_label.trim(),
    agent_id: f.fallback === 'agent' ? f.agent_id || null : null,
    selector: f.fallback === 'labels' ? splitList(f.selector) : [],
    autonomy: f.autonomy,
    instructions: f.instructions,
  }
}

const dupKeys = computed(() => {
  const seen = new Set<string>()
  const dups = new Set<string>()
  for (const p of form.value.match) {
    const k = p.k.trim()
    if (!k) continue
    if (seen.has(k)) dups.add(k)
    seen.add(k)
  }
  return dups
})
const errors = computed(() => ({
  name: !form.value.name.trim(),
  agent: form.value.fallback === 'agent' && !form.value.agent_id,
  labels: form.value.fallback === 'labels' && !splitList(form.value.selector).length,
  match: dupKeys.value.size > 0,
}))
const valid = computed(() => !Object.values(errors.value).some(Boolean))
const matchesEverything = computed(() => !form.value.match.some((p) => p.k.trim()))
const agents = computed(() => catalog.agents.filter((a) => a.status !== 'revoked'))

function openNew() {
  editing.value = null
  form.value = blank()
  tried.value = false
  show.value = true
}

function openEdit(r: AlertRoute) {
  editing.value = r
  form.value = formFrom(r)
  tried.value = false
  show.value = true
}

async function save() {
  tried.value = true
  if (!valid.value) return
  saving.value = true
  try {
    if (editing.value) {
      const r = await api.updateAlertRoute(editing.value.id, inputFrom(form.value))
      replace(r)
      toast.success(`Alert route ${r.name} saved`)
    } else {
      const created = await api.createAlertRoute(inputFrom(form.value))
      routes.value = [...routes.value, created.route].sort((a, b) => a.name.localeCompare(b.name))
      secret.value = { ...created, rotated: false }
    }
    show.value = false
  } catch {
    /* toasted */
  } finally {
    saving.value = false
  }
}

function replace(r: AlertRoute) {
  const i = routes.value.findIndex((x) => x.id === r.id)
  if (i >= 0) routes.value[i] = r
}

async function toggle(r: AlertRoute) {
  busy.value = r.id
  try {
    replace(await api.updateAlertRoute(r.id, { ...inputFrom(formFrom(r)), enabled: !r.enabled }))
    toast.success(r.enabled ? `${r.name} disabled: its webhook now answers 404` : `${r.name} enabled`)
  } catch {
    /* toasted */
  } finally {
    busy.value = null
  }
}

async function rotate(r: AlertRoute) {
  const ok = await confirm.ask({
    title: `Rotate the webhook URL of ${r.name}?`,
    message: 'A new secret URL is issued and the current one stops working immediately. Update Alertmanager (or whatever sends the alerts) with the new URL.',
    confirmText: 'Rotate URL',
    danger: true,
  })
  if (!ok) return
  busy.value = r.id
  try {
    const res = await api.rotateAlertRoute(r.id)
    // The rotate response carries the route as it was loaded, before the new token prefix was saved.
    await load()
    secret.value = { ...res, route: routes.value.find((x) => x.id === r.id) ?? res.route, rotated: true }
  } catch {
    /* toasted */
  } finally {
    busy.value = null
  }
}

async function remove(r: AlertRoute) {
  const ok = await confirm.ask({
    title: `Delete alert route ${r.name}?`,
    message: 'Its webhook URL stops working. Triage tasks it already created are kept.',
    confirmText: `Delete ${r.name}`,
    danger: true,
  })
  if (!ok) return
  busy.value = r.id
  try {
    await api.deleteAlertRoute(r.id)
    routes.value = routes.value.filter((x) => x.id !== r.id)
    toast.success('Alert route deleted')
  } catch {
    /* toasted */
  } finally {
    busy.value = null
  }
}

function target(r: AlertRoute): string {
  const fb = r.agent_id ? catalog.agentName(r.agent_id) : r.selector?.length ? `agents labelled ${r.selector.join(', ')}` : 'any agent'
  return fb
}

// ---- one-time webhook URL -----------------------------------------------------------------------

const secret = ref<(AlertRouteCreated & { rotated: boolean }) | null>(null)

const yaml = computed(() => {
  const s = secret.value
  if (!s) return ''
  const match = Object.entries(s.route.match ?? {})
  const lines = [
    'receivers:',
    '  - name: akili',
    '    webhook_configs:',
    `      - url: '${s.webhook_url}'`,
    '        send_resolved: false',
    '',
    'route:',
    '  routes:',
    '    - receiver: akili',
  ]
  if (match.length) {
    lines.push('      matchers:')
    for (const [k, v] of match) lines.push(`        - ${k}="${v.replace(/"/g, '\\"')}"`)
  }
  lines.push('      continue: true   # keep notifying your other receivers too')
  return lines.join('\n')
})

const curl = computed(() => {
  const s = secret.value
  if (!s) return ''
  const labels: Record<string, string> = { ...(s.route.match ?? {}) }
  const hostLabel = s.route.host_label || 'instance'
  if (hostLabel !== 'instance') labels[hostLabel] = 'web-1'
  const body = {
    title: 'Disk almost full',
    description: '/var is 97% full',
    severity: labels.severity ?? 'critical',
    host: 'web-1',
    labels,
    fingerprint: 'disk-web-1',
  }
  const json = JSON.stringify(body, null, 2).replace(/'/g, "'\\''")
  return `curl -X POST '${s.webhook_url}' \\\n  -H 'Content-Type: application/json' \\\n  -d '${json}'`
})

async function copy(text: string, what: string) {
  if (await copyText(text)) toast.success(`${what} copied`)
}

onMounted(() => {
  load()
  catalog.loadAgents()
})
</script>

<template>
  <div>
    <PageHeader title="Alerts" subtitle="Alert routes turn alerts from Alertmanager (or any JSON sender) into triage tasks on the affected host. Fixes still need an approved change plan.">
      <button type="button" class="btn btn-primary" @click="openNew"><Icon name="plus" />New alert route</button>
    </PageHeader>

    <div class="card">
      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr>
              <th>Route</th><th>Enabled</th><th>Matches</th><th class="hide-mobile">Target</th><th class="hide-mobile">Autonomy</th><th class="hide-mobile">Last alert</th>
              <th class="hide-mobile">Token</th><th><span class="sr-only">Actions</span></th>
            </tr>
          </thead>
          <tbody>
            <SkeletonRows v-if="loading" :cols="8" :rows="3" />
            <tr v-else-if="!routes.length">
              <td colspan="8">
                <EmptyState title="No alert routes yet" icon="siren">
                  Create a route, point Alertmanager at its secret URL, and every matching alert becomes a triage task: the agent on the alerting host investigates and proposes a fix for you to approve.
                  <template #actions><button type="button" class="btn btn-primary" @click="openNew"><Icon name="plus" />New alert route</button></template>
                </EmptyState>
              </td>
            </tr>
            <tr v-for="r in routes" :key="r.id" :class="{ 'row-hl': highlight === r.id }">
              <td>
                <div class="cell-title">{{ r.name }}</div>
                <div v-if="r.instructions" class="cell-sub truncate" style="max-width: 260px">{{ r.instructions.split('\n')[0] }}</div>
              </td>
              <td>
                <label class="switch">
                  <input type="checkbox" :checked="r.enabled" :disabled="busy === r.id" :aria-label="`Enable ${r.name}`" @change="toggle(r)" />
                  <span class="sr-only">{{ r.enabled ? 'enabled' : 'disabled' }}</span>
                </label>
              </td>
              <td>
                <span v-for="(v, k) in r.match ?? {}" :key="k" class="label-chip mono">{{ k }}={{ v }}</span>
                <span v-if="!Object.keys(r.match ?? {}).length" class="small muted">every alert</span>
              </td>
              <td class="hide-mobile">
                <div class="small">host from <code class="mono">{{ r.host_label || 'instance' }}</code></div>
                <div class="cell-sub">else {{ target(r) }}</div>
              </td>
              <td class="hide-mobile"><span class="badge outline square" :title="AUTONOMY_LEVELS[r.autonomy]?.help">L{{ r.autonomy }}</span></td>
              <td class="nowrap hide-mobile" :title="fmtDate(r.last_alert_at)">{{ relTime(r.last_alert_at, now) }}</td>
              <td class="hide-mobile"><code class="mono small muted">{{ r.token_prefix }}…</code></td>
              <td class="right nowrap">
                <div class="row end" style="gap: 4px">
                  <button type="button" class="btn btn-sm btn-ghost btn-icon" :aria-label="`Edit ${r.name}`" title="Edit" :disabled="busy === r.id" @click="openEdit(r)"><Icon name="edit" /></button>
                  <button type="button" class="btn btn-sm btn-ghost btn-icon" :aria-label="`Rotate the webhook URL of ${r.name}`" title="Rotate webhook URL" :disabled="busy === r.id" @click="rotate(r)"><Icon name="key" /></button>
                  <button type="button" class="btn btn-sm btn-ghost btn-icon btn-danger-ghost" :aria-label="`Delete ${r.name}`" title="Delete" :disabled="busy === r.id" @click="remove(r)"><Icon name="trash" /></button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- create / edit -->
    <Modal :open="show" :title="editing ? `Edit ${editing.name}` : 'New alert route'" wide :dismissable="!saving" @close="show = false">
      <form id="route-form" class="stack loose" novalidate @submit.prevent="save">
        <div class="grid-2">
          <div class="field">
            <label for="ar-name">Name <span class="req" aria-hidden="true">*</span></label>
            <input id="ar-name" v-model="form.name" class="input" :class="{ invalid: tried && errors.name }" maxlength="120" required placeholder="e.g. critical-infra" />
            <span v-if="tried && errors.name" class="error-msg"><Icon name="alert" />Name the route.</span>
          </div>
          <label class="switch" style="align-self: end; padding-bottom: 8px"><input v-model="form.enabled" type="checkbox" />Enabled: accepts alerts</label>
        </div>

        <fieldset class="rule-block stack tight" style="margin: 0">
          <legend class="strong" style="padding: 0 4px">Match labels</legend>
          <p class="xs muted" style="margin: 0">An alert must carry every label below with exactly this value. Resolved alerts are always ignored.</p>
          <div v-for="(p, i) in form.match" :key="i" class="row pair">
            <label :for="`ar-k-${i}`" class="sr-only">Label {{ i + 1 }} name</label>
            <input :id="`ar-k-${i}`" v-model="p.k" class="input mono sm" :class="{ invalid: dupKeys.has(p.k.trim()) }" placeholder="severity" spellcheck="false" />
            <span class="muted" aria-hidden="true">=</span>
            <label :for="`ar-v-${i}`" class="sr-only">Label {{ i + 1 }} value</label>
            <input :id="`ar-v-${i}`" v-model="p.v" class="input mono sm" placeholder="critical" spellcheck="false" />
            <button type="button" class="btn btn-ghost btn-icon btn-sm" :aria-label="`Remove label ${p.k || i + 1}`" @click="form.match.splice(i, 1)"><Icon name="x" /></button>
          </div>
          <div class="row">
            <button type="button" class="btn btn-sm" @click="form.match.push({ k: '', v: '' })"><Icon name="plus" />Add label</button>
            <span v-if="dupKeys.size" class="error-msg"><Icon name="alert" />Each label can appear once.</span>
            <span v-else-if="matchesEverything" class="hint warn">No labels: every firing alert sent to this URL creates a task.</span>
          </div>
        </fieldset>

        <fieldset class="rule-block stack" style="margin: 0">
          <legend class="strong" style="padding: 0 4px">Where it runs</legend>
          <div class="field">
            <label for="ar-host">Host label</label>
            <input id="ar-host" v-model="form.host_label" class="input mono" placeholder="instance" spellcheck="false" />
            <span class="hint">The alert label naming the affected host (Prometheus uses <code>instance</code>, e.g. <code>web-1:9100</code>). The agent whose name or hostname matches runs the triage.</span>
          </div>
          <div class="field">
            <span id="ar-fb" class="label">If no agent matches the host</span>
            <div class="choices" role="radiogroup" aria-labelledby="ar-fb">
              <label class="choice" :class="{ on: form.fallback === 'agent' }">
                <input v-model="form.fallback" type="radio" name="ar-fb" value="agent" /><Icon name="agents" />
                <span><span class="c-title">A specific agent</span><br /><span class="c-sub">e.g. a monitoring host.</span></span>
              </label>
              <label class="choice" :class="{ on: form.fallback === 'labels' }">
                <input v-model="form.fallback" type="radio" name="ar-fb" value="labels" /><Icon name="layers" />
                <span><span class="c-title">By labels</span><br /><span class="c-sub">Any agent with every label.</span></span>
              </label>
              <label class="choice" :class="{ on: form.fallback === 'any' }">
                <input v-model="form.fallback" type="radio" name="ar-fb" value="any" /><Icon name="globe" />
                <span><span class="c-title">Any agent</span><br /><span class="c-sub">First available.</span></span>
              </label>
            </div>
          </div>
          <div v-if="form.fallback === 'agent'" class="field">
            <label for="ar-agent">Fallback agent</label>
            <select id="ar-agent" v-model="form.agent_id" class="select" :class="{ invalid: tried && errors.agent }">
              <option value="" disabled>Select an agent</option>
              <option v-for="a in agents" :key="a.id" :value="a.id">{{ a.name }} · {{ a.status }}</option>
            </select>
            <span v-if="tried && errors.agent" class="error-msg"><Icon name="alert" />Pick the fallback agent.</span>
          </div>
          <div v-else-if="form.fallback === 'labels'" class="field">
            <label for="ar-sel">Agent labels</label>
            <input id="ar-sel" v-model="form.selector" class="input mono" :class="{ invalid: tried && errors.labels }" placeholder="prod, web" />
            <span v-if="tried && errors.labels" class="error-msg"><Icon name="alert" />Enter at least one label.</span>
            <span v-else class="hint">Comma separated.</span>
          </div>
        </fieldset>

        <div>
          <AutonomySelect id="ar-aut" v-model="form.autonomy" />
          <div class="banner info" style="margin-top: 8px">
            <Icon name="approvals" />
            <div class="banner-body">L2 lets the agent investigate on its own. Whatever the level, a <strong>fix still needs an approved change plan</strong>: steps, checks and a rollback that a human approves as a whole.</div>
          </div>
        </div>

        <div class="field">
          <label for="ar-instr">Instructions <span class="opt">(optional)</span></label>
          <textarea id="ar-instr" v-model="form.instructions" class="textarea" rows="5" placeholder="Runbook hints, escalation contacts, what never to touch…" />
          <span class="hint">Added to every triage goal from this route. The alert itself is passed to the agent as data, never as instructions.</span>
        </div>
      </form>
      <template #footer>
        <button type="button" class="btn" @click="show = false">Cancel</button>
        <button type="submit" form="route-form" class="btn btn-primary" :disabled="saving">
          <span v-if="saving" class="spinner" />{{ editing ? 'Save changes' : 'Create route' }}
        </button>
      </template>
    </Modal>

    <!-- one-time URL -->
    <Modal :open="!!secret" :title="secret?.rotated ? 'New webhook URL' : 'Alert route created'" wide :dismissable="false" @close="secret = null">
      <div v-if="secret" class="stack loose">
        <div class="banner warn" role="alert">
          <Icon name="key" />
          <div class="banner-body">
            <strong>Copy this URL now: it is shown only once.</strong>
            <p>The URL contains the route's secret token{{ secret.rotated ? '; the previous URL no longer works' : '' }}. Anyone with it can create triage tasks, so treat it like a password. Lost it? Rotate to get a new one.</p>
          </div>
        </div>
        <CopyField :value="secret.webhook_url" label="Webhook URL" />

        <div class="field">
          <div class="row between">
            <span class="label">Alertmanager receiver</span>
            <button type="button" class="btn btn-xs" @click="copy(yaml, 'Alertmanager snippet')"><Icon name="copy" />Copy</button>
          </div>
          <pre class="code nowrap">{{ yaml }}</pre>
          <span class="hint">Add to <code>alertmanager.yml</code>. <code>send_resolved: false</code>: Akili ignores resolved notifications.</span>
        </div>

        <div class="field">
          <div class="row between">
            <span class="label">Or send a generic JSON alert</span>
            <button type="button" class="btn btn-xs" @click="copy(curl, 'curl example')"><Icon name="copy" />Copy</button>
          </div>
          <pre class="code nowrap">{{ curl }}</pre>
          <span class="hint">Fields: <code>title</code> (required), <code>description</code>, <code>severity</code>, <code>host</code>, <code>labels</code>, <code>fingerprint</code> (dedupes while a triage task is open).</span>
        </div>
        <div class="small muted">
          Route <strong>{{ secret.route.name }}</strong> · <Badge :value="secret.route.enabled ? 'enabled' : 'disabled'" /> · token <code class="mono">{{ secret.route.token_prefix }}…</code>
        </div>
      </div>
      <template #footer>
        <button type="button" class="btn btn-primary" @click="secret = null"><Icon name="check" />I've copied it</button>
      </template>
    </Modal>
  </div>
</template>

<style scoped>
.pair .input {
  flex: 1;
  min-width: 0;
}
.row-hl td {
  background: var(--bg-active);
}
</style>
