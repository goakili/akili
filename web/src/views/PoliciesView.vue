<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, AUTONOMY_LEVELS, RISKS, type Autonomy, type Decision, type Policy, type PolicyDocument, type Risk, type ToolSpec } from '../api'
import { useAuth } from '../stores/auth'
import { useConfirm } from '../stores/confirm'
import { useToast } from '../stores/toast'
import { fmtDate, prettyJSON } from '../lib/format'
import { copyText } from '../lib/clipboard'
import { toolGroup } from '../lib/tools'
import Badge from '../components/Badge.vue'
import PageHeader from '../components/PageHeader.vue'
import EmptyState from '../components/EmptyState.vue'
import Icon from '../components/Icon'

const auth = useAuth()
const confirm = useConfirm()
const toast = useToast()
const route = useRoute()
const router = useRouter()

const policies = ref<Policy[]>([])
const tools = ref<ToolSpec[]>([])
const selected = ref<Policy | null>(null)
const creating = ref(false)
const loading = ref(true)
const saving = ref(false)
const jsonText = ref('')
const jsonError = ref('')
const jsonFocused = ref(false)
const tried = ref(false)

type RuleKey = 'tools' | 'paths' | 'commands' | 'domains' | 'services' | 'containers' | 'apps'
const RULES: { key: RuleKey; title: string; help: string; allowPh: string; denyPh: string; hint?: string }[] = [
  { key: 'tools', title: 'Tools', help: 'Tool names; * is a wildcard.', allowPh: 'fs_read\nfs_list\nshell', denyPh: 'fs_write' },
  { key: 'paths', title: 'Paths', help: '$WORKDIR expands to the agent workdir; ** crosses directories.', allowPh: '$WORKDIR/**\n/var/log/**', denyPh: '/etc/shadow\n**/.ssh/**' },
  { key: 'commands', title: 'Commands', help: 'Matched against the whole command line; * matches anything.', allowPh: 'systemctl status *\ndf -h', denyPh: 'rm -rf *' },
  { key: 'domains', title: 'Domains', help: 'Hosts http_fetch may reach; *.example.com matches subdomains.', allowPh: 'api.github.com\n*.example.com', denyPh: '' },
  { key: 'services', title: 'Services', help: 'systemd units the service tools may touch; bare names get .service.', allowPh: 'nginx.service\napp-*', denyPh: 'sshd.service' },
  { key: 'containers', title: 'Containers', help: 'Container names the docker tools may touch; * is a wildcard.', allowPh: 'web-*\nredis', denyPh: 'postgres' },
  {
    key: 'apps',
    title: 'Miabi resources',
    help: 'Matched as workspace/app or workspace/db:<name> (also stack:, cron:, pipeline:); * is a wildcard.',
    allowPh: 'staging/*\nprod/api\nprod/db:*',
    denyPh: 'prod/db:billing',
    hint: 'Examples: staging/* (everything in staging), prod/api, prod/db:* (every prod database). prod/* covers every kind: apps, databases, stacks, cron jobs, pipelines and workspace-level tools such as alerts.',
  },
]

interface Form {
  name: string
  description: string
  rules: Record<RuleKey, { allow: string; deny: string }>
  max_risk: Risk
  require_approval: string
  allow_shell_meta: boolean
  terminal: boolean
  version: number
}

const emptyRules = (): Form['rules'] => ({
  tools: { allow: '', deny: '' },
  paths: { allow: '', deny: '' },
  commands: { allow: '', deny: '' },
  domains: { allow: '', deny: '' },
  services: { allow: '', deny: '' },
  containers: { allow: '', deny: '' },
  apps: { allow: '', deny: '' },
})
const form = ref<Form>({ name: '', description: '', rules: emptyRules(), max_risk: 'medium', require_approval: '', allow_shell_meta: false, terminal: false, version: 1 })
const baseline = ref('')

const lines = (s: string) => s.split('\n').map((x) => x.trim()).filter(Boolean)
const join = (a?: string[] | null) => (a ?? []).join('\n')

function formFrom(p: { name: string; description: string; document: PolicyDocument }): Form {
  const d = p.document
  const rules = emptyRules()
  for (const r of RULES) rules[r.key] = { allow: join(d[r.key]?.allow), deny: join(d[r.key]?.deny) }
  return {
    name: p.name,
    description: p.description,
    rules,
    max_risk: (RISKS as string[]).includes(d.max_risk) ? (d.max_risk as Risk) : 'medium',
    require_approval: join(d.require_approval),
    allow_shell_meta: !!d.allow_shell_meta,
    terminal: !!d.terminal,
    version: d.version || 1,
  }
}

function docFrom(f: Form): PolicyDocument {
  const rule = (k: RuleKey) => ({ allow: lines(f.rules[k].allow), deny: lines(f.rules[k].deny) })
  return {
    name: f.name.trim(),
    version: f.version,
    tools: rule('tools'),
    paths: rule('paths'),
    commands: rule('commands'),
    domains: rule('domains'),
    services: rule('services'),
    containers: rule('containers'),
    apps: rule('apps'),
    terminal: f.terminal,
    max_risk: f.max_risk,
    require_approval: lines(f.require_approval),
    allow_shell_meta: f.allow_shell_meta,
  }
}

const readonly = computed(() => !auth.isAdmin || (!!selected.value?.builtin && !creating.value))
const editing = computed(() => creating.value || !!selected.value)
const snapshot = () => JSON.stringify({ n: form.value.name, d: form.value.description, doc: docFrom(form.value) })
const dirty = computed(() => editing.value && !readonly.value && (creating.value || snapshot() !== baseline.value))

// Live JSON: the form drives the JSON pane; typing valid JSON in the pane drives the form.
const liveDoc = computed(() => prettyJSON(docFrom(form.value)))
watch(liveDoc, (t) => {
  if (!jsonFocused.value) {
    jsonText.value = t
    jsonError.value = ''
  }
})

function onJsonInput() {
  try {
    const doc = JSON.parse(jsonText.value) as PolicyDocument
    if (!doc || typeof doc !== 'object' || Array.isArray(doc)) throw new Error('The document must be a JSON object.')
    const f = formFrom({ name: typeof doc.name === 'string' ? doc.name : form.value.name, description: form.value.description, document: doc })
    form.value = f
    jsonError.value = ''
  } catch (e) {
    jsonError.value = e instanceof Error ? e.message : 'Invalid JSON'
  }
}
function onJsonBlur() {
  jsonFocused.value = false
  if (!jsonError.value) jsonText.value = liveDoc.value
}

async function load() {
  try {
    const [p, t] = await Promise.all([api.listPolicies(), api.listTools()])
    policies.value = p ?? []
    tools.value = t ?? []
    if (!simTool.value && tools.value.length) simTool.value = tools.value[0].name
  } catch {
    /* toasted */
  } finally {
    loading.value = false
  }
}

async function guardDirty() {
  if (!dirty.value) return true
  return confirm.ask({ title: 'Discard unsaved changes?', message: `Your edits to ${form.value.name || 'this policy'} will be lost.`, confirmText: 'Discard changes', danger: true })
}

function setForm(f: Form) {
  form.value = f
  baseline.value = snapshot()
  jsonText.value = prettyJSON(docFrom(f))
  jsonError.value = ''
  tried.value = false
}

async function select(p: Policy) {
  if (selected.value?.id === p.id && !creating.value) return
  if (!(await guardDirty())) return
  creating.value = false
  selected.value = p
  setForm(formFrom(p))
  simResult.value = null
}

async function startNew() {
  if (!(await guardDirty())) return
  selected.value = null
  creating.value = true
  setForm({ name: '', description: '', rules: { ...emptyRules(), paths: { allow: '$WORKDIR/**', deny: '' } }, max_risk: 'medium', require_approval: '', allow_shell_meta: false, terminal: false, version: 1 })
}

function duplicate() {
  const p = selected.value
  if (!p) return
  const f = formFrom(p)
  f.name = `${p.name}-copy`
  f.version = 1
  selected.value = null
  creating.value = true
  setForm(f)
  toast.info(`Editing a copy of ${p.name}. Save to create it.`)
}

async function copyJSON() {
  if (await copyText(jsonText.value)) toast.success('Policy JSON copied')
}

async function save() {
  tried.value = true
  if (jsonError.value) return
  if (!form.value.name.trim()) return
  saving.value = true
  const body = { name: form.value.name.trim(), description: form.value.description, document: docFrom(form.value) }
  try {
    const p = creating.value ? await api.createPolicy(body) : await api.updatePolicy(selected.value!.id, body)
    toast.success(creating.value ? `Policy ${p.name} created` : `Saved ${p.name} (v${p.version})`)
    creating.value = false
    await load()
    selected.value = policies.value.find((x) => x.id === p.id) ?? p
    setForm(formFrom(selected.value))
  } catch {
    /* toasted */
  } finally {
    saving.value = false
  }
}

async function remove() {
  const p = selected.value
  if (!p) return
  if (!(await confirm.ask({ title: `Delete policy ${p.name}?`, message: 'Policies bound to agents cannot be deleted.', confirmText: `Delete ${p.name}`, danger: true }))) return
  try {
    await api.deletePolicy(p.id)
    selected.value = null
    toast.success(`Policy ${p.name} deleted`)
    load()
  } catch {
    /* toasted */
  }
}

// ---- simulate ------------------------------------------------------------------------------------

const simTool = ref('')
const simInput = ref('{}')
const simAutonomy = ref<Autonomy>(1)
const simWorkdir = ref('')
const simResult = ref<Decision | null>(null)
const simError = ref('')
const simBusy = ref(false)
const toolGroups = computed(() => {
  const groups = new Map<string, ToolSpec[]>()
  for (const t of tools.value) {
    const g = toolGroup(t.name)
    groups.set(g, [...(groups.get(g) ?? []), t])
  }
  return [...groups].map(([label, tools]) => ({ label, tools }))
})
const simSpec = computed(() => tools.value.find((t) => t.name === simTool.value))

watch(simTool, () => {
  const props = simSpec.value?.input_schema?.properties ?? {}
  const skeleton: Record<string, unknown> = {}
  for (const [k, v] of Object.entries(props)) {
    skeleton[k] = v.type === 'integer' || v.type === 'number' ? 0 : v.type === 'boolean' ? false : v.type === 'array' ? [] : ''
  }
  simInput.value = JSON.stringify(skeleton, null, 2)
  simResult.value = null
})

async function simulate() {
  if (!selected.value) return
  simError.value = ''
  let input: unknown
  try {
    input = JSON.parse(simInput.value || '{}')
  } catch {
    simError.value = 'Input is not valid JSON.'
    return
  }
  simBusy.value = true
  try {
    simResult.value = await api.simulatePolicy(selected.value.id, { tool: simTool.value, input, autonomy: simAutonomy.value, workdir: simWorkdir.value.trim() || undefined })
  } catch {
    /* toasted */
  } finally {
    simBusy.value = false
  }
}

onMounted(async () => {
  await load()
  if (route.query.new && auth.isAdmin) {
    router.replace({ query: {} })
    startNew()
  } else if (!selected.value && policies.value.length) {
    select(policies.value[0])
  }
})
</script>

<template>
  <div>
    <PageHeader title="Policies" subtitle="Default-deny capability policies. Every tool call is checked against the agent’s policy on the control plane.">
      <button v-if="auth.isAdmin" type="button" class="btn btn-primary" @click="startNew"><Icon name="plus" />New policy</button>
    </PageHeader>

    <div class="split narrow-side">
      <nav class="card side-list" style="padding: 6px" aria-label="Policies">
        <template v-if="loading">
          <div v-for="i in 4" :key="i" class="list-row"><span class="skel" style="width: 70%" /></div>
        </template>
        <div v-else-if="!policies.length" class="empty compact"><strong>No policies</strong></div>
        <button
          v-if="creating"
          type="button"
          class="list-row active"
          aria-current="true"
        >
          <div class="grow">
            <div class="truncate strong">{{ form.name || 'New policy' }}</div>
            <div class="xs muted">unsaved</div>
          </div>
          <span class="badge warn">draft</span>
        </button>
        <button
          v-for="p in policies"
          :key="p.id"
          type="button"
          class="list-row"
          :class="{ active: selected?.id === p.id && !creating }"
          :aria-current="selected?.id === p.id && !creating ? 'true' : undefined"
          @click="select(p)"
        >
          <div class="grow">
            <div class="truncate strong">{{ p.name }}</div>
            <div class="xs muted truncate">{{ p.description || `max risk: ${p.document.max_risk}` }}</div>
          </div>
          <Badge v-if="p.builtin" value="builtin" />
          <span v-else class="badge outline square">v{{ p.version }}</span>
        </button>
      </nav>

      <div v-if="!editing" class="card">
        <EmptyState title="Select a policy" icon="policies">Pick a policy on the left to view, edit or simulate it.</EmptyState>
      </div>
      <div v-else class="stack loose">
        <form class="card" novalidate @submit.prevent="save">
          <div class="card-head" style="flex-wrap: wrap">
            <div class="row wrap">
              <h2>{{ creating ? (form.name ? form.name : 'New policy') : selected?.name }}</h2>
              <Badge v-if="selected?.builtin && !creating" value="builtin" label="built-in · read-only" />
              <span v-else-if="selected && !creating" class="small muted">v{{ selected.version }} · updated {{ fmtDate(selected.updated_at) }}</span>
              <span v-if="dirty && !creating" class="badge warn">unsaved</span>
            </div>
            <div class="row">
              <button v-if="selected && !creating && auth.isAdmin" type="button" class="btn btn-sm" :class="{ 'btn-primary': selected.builtin }" @click="duplicate">
                <Icon name="copy" />Duplicate{{ selected.builtin ? ' to edit' : '' }}
              </button>
            </div>
          </div>
          <div v-if="selected?.builtin && !creating" class="banner info" style="margin: 14px 18px 0; border-radius: var(--radius)">
            <Icon name="lock" />
            <div class="banner-body">Built-in policies are maintained by Akili and cannot be edited. Duplicate this one to customise it.</div>
          </div>

          <div class="editor-split">
            <fieldset class="pane card-body stack loose" :disabled="readonly || saving" style="border: 0; margin: 0; min-width: 0">
              <div class="grid-2">
                <div class="field">
                  <label for="po-name">Name <span class="req" aria-hidden="true">*</span></label>
                  <input id="po-name" v-model="form.name" class="input" :class="{ invalid: tried && !form.name.trim() }" required maxlength="120" placeholder="e.g. web-readonly" />
                  <span v-if="tried && !form.name.trim()" class="error-msg"><Icon name="alert" />Name the policy.</span>
                </div>
                <div class="field">
                  <label for="po-desc">Description <span class="opt">(optional)</span></label>
                  <input id="po-desc" v-model="form.description" class="input" maxlength="500" />
                </div>
                <div class="field">
                  <label for="po-risk">Max risk</label>
                  <select id="po-risk" v-model="form.max_risk" class="select">
                    <option v-for="r in RISKS" :key="r" :value="r">{{ r }}</option>
                  </select>
                  <span class="hint">Nothing above this ever runs, even with approval.</span>
                </div>
                <div class="field">
                  <label for="po-req">Always require approval for</label>
                  <textarea id="po-req" v-model="form.require_approval" class="textarea mono" rows="2" placeholder="shell&#10;fs_write" />
                  <span class="hint">Tool globs, one per line, regardless of autonomy.</span>
                </div>
              </div>
              <div v-for="r in RULES" :key="r.key" class="rule-block stack tight">
                <div class="row between wrap">
                  <h3>{{ r.title }}</h3>
                  <span class="xs muted">{{ r.help }}</span>
                </div>
                <div class="grid-2" style="gap: 10px">
                  <div class="field">
                    <label :for="`po-${r.key}-allow`" class="row" style="gap: 5px"><Icon name="check" :size="13" style="color: var(--success-text)" />Allow</label>
                    <textarea :id="`po-${r.key}-allow`" v-model="form.rules[r.key].allow" class="textarea mono" rows="3" :placeholder="r.allowPh" spellcheck="false" />
                  </div>
                  <div class="field">
                    <label :for="`po-${r.key}-deny`" class="row" style="gap: 5px"><Icon name="ban" :size="13" style="color: var(--danger-text)" />Deny <span class="opt">(always wins)</span></label>
                    <textarea :id="`po-${r.key}-deny`" v-model="form.rules[r.key].deny" class="textarea mono" rows="3" :placeholder="r.denyPh" spellcheck="false" />
                  </div>
                </div>
                <span v-if="r.hint" class="hint">{{ r.hint }}</span>
              </div>
              <label class="check">
                <input v-model="form.allow_shell_meta" type="checkbox" />
                <span>Allow shell metacharacters <span class="muted small">(; &amp; | ` $( &lt; &gt;, off is strongly recommended)</span></span>
              </label>
              <div class="rule-block stack tight" :class="{ 'term-on': form.terminal }">
                <label class="switch">
                  <input v-model="form.terminal" type="checkbox" aria-describedby="po-term-help" />
                  <span class="strong">Terminal</span>
                  <span class="muted small">recorded interactive shell for admins</span>
                </label>
                <div id="po-term-help" class="banner" :class="form.terminal ? 'warn' : 'info'" style="margin: 0">
                  <Icon :name="form.terminal ? 'alert' : 'terminal'" />
                  <div class="banner-body small">
                    <template v-if="form.terminal"><strong>Grants an interactive shell to admins.</strong> The tool, path and command rules above do not apply inside it: whoever opens it can do anything the agent's user can. Every keystroke and output is recorded to the audit trail.</template>
                    <template v-else>Off: nobody can open a terminal on agents bound to this policy. Turning it on lets admins open a recorded shell as the agent's user.</template>
                  </div>
                </div>
              </div>
            </fieldset>

            <div class="pane json-pane">
              <div class="jp-head">
                <span class="row" style="gap: 6px"><Icon name="terminal" :size="14" />Policy document (JSON){{ readonly ? '' : ' · editable' }}</span>
                <button type="button" class="btn btn-xs copy-btn" style="background: rgb(255 255 255 / 10%); color: #e5e7eb; border-color: rgb(255 255 255 / 14%); box-shadow: none" @click="copyJSON">
                  <Icon name="copy" />Copy
                </button>
              </div>
              <label for="po-json" class="sr-only">Policy document as JSON</label>
              <textarea
                id="po-json"
                v-model="jsonText"
                spellcheck="false"
                :readonly="readonly"
                :aria-invalid="jsonError ? 'true' : undefined"
                @focus="jsonFocused = true"
                @blur="onJsonBlur"
                @input="onJsonInput"
              />
              <div v-if="jsonError" class="jp-error" role="alert">{{ jsonError }}</div>
            </div>
          </div>

          <div v-if="!readonly" class="card-foot" style="justify-content: space-between">
            <button v-if="selected && !creating" type="button" class="btn btn-danger-ghost" @click="remove"><Icon name="trash" />Delete</button>
            <button v-else type="button" class="btn" @click="(creating = false), selected ? setForm(formFrom(selected)) : null">Cancel</button>
            <button type="submit" class="btn btn-primary" :disabled="saving || !!jsonError || (!creating && !dirty)">
              <span v-if="saving" class="spinner" />{{ saving ? 'Saving…' : creating ? 'Create policy' : 'Save changes' }}
            </button>
          </div>
        </form>

        <section v-if="selected && !creating" class="card" aria-labelledby="sim-title">
          <div class="card-head">
            <h2 id="sim-title"><Icon name="play" />Simulate a tool call</h2>
            <span class="small muted hide-mobile">Would this call be allowed under the saved version?</span>
          </div>
          <form class="card-body stack" @submit.prevent="simulate">
            <div class="grid-3">
              <div class="field">
                <label for="sim-tool">Tool</label>
                <select id="sim-tool" v-model="simTool" class="select">
                  <optgroup v-for="g in toolGroups" :key="g.label" :label="g.label">
                    <option v-for="t in g.tools" :key="t.name" :value="t.name">{{ t.name }} ({{ t.risk }})</option>
                  </optgroup>
                </select>
              </div>
              <div class="field">
                <label for="sim-aut">Autonomy</label>
                <select id="sim-aut" v-model.number="simAutonomy" class="select">
                  <option v-for="l in AUTONOMY_LEVELS" :key="l.value" :value="l.value">{{ l.label }}</option>
                </select>
              </div>
              <div class="field">
                <label for="sim-wd">Workdir <span class="opt">(optional)</span></label>
                <input id="sim-wd" v-model="simWorkdir" class="input mono" placeholder="/var/lib/akili-agent/work" />
              </div>
            </div>
            <div v-if="simSpec" class="small muted">{{ simSpec.description }}</div>
            <div class="field">
              <label for="sim-input">Input (JSON)</label>
              <textarea id="sim-input" v-model="simInput" class="textarea mono" rows="5" spellcheck="false" />
            </div>
            <div v-if="simError" class="form-error" role="alert"><Icon name="alert" />{{ simError }}</div>
            <div class="row">
              <button type="submit" class="btn btn-primary" :disabled="simBusy || !simTool"><span v-if="simBusy" class="spinner" /><Icon v-else name="play" />Simulate</button>
            </div>
            <div
              v-if="simResult"
              class="banner"
              :class="simResult.effect === 'allow' ? 'ok' : simResult.effect === 'deny' ? 'danger' : 'warn'"
              role="status"
            >
              <Icon :name="simResult.effect === 'allow' ? 'checkCircle' : simResult.effect === 'deny' ? 'ban' : 'approvals'" />
              <div class="banner-body stack tight">
                <div class="row wrap">
                  <Badge :value="simResult.effect" kind="effect" />
                  <Badge v-if="simResult.risk && simResult.risk !== 'unknown'" :value="simResult.risk" kind="risk" />
                  <span style="color: var(--text-primary)">{{ simResult.reason }}</span>
                </div>
                <dl v-if="simResult.resources && (simResult.resources.paths?.length || simResult.resources.commands?.length || simResult.resources.domains?.length || simResult.resources.services?.length || simResult.resources.containers?.length || simResult.resources.apps?.length)" class="kv" style="color: var(--text-primary)">
                  <template v-if="simResult.resources.paths?.length"><dt>Paths</dt><dd class="mono small">{{ simResult.resources.paths.join(', ') }}</dd></template>
                  <template v-if="simResult.resources.commands?.length"><dt>Commands</dt><dd class="mono small">{{ simResult.resources.commands.join(' · ') }}</dd></template>
                  <template v-if="simResult.resources.domains?.length"><dt>Domains</dt><dd class="mono small">{{ simResult.resources.domains.join(', ') }}</dd></template>
                  <template v-if="simResult.resources.services?.length"><dt>Services</dt><dd class="mono small">{{ simResult.resources.services.join(', ') }}</dd></template>
                  <template v-if="simResult.resources.containers?.length"><dt>Containers</dt><dd class="mono small">{{ simResult.resources.containers.join(', ') }}</dd></template>
                  <template v-if="simResult.resources.apps?.length"><dt>Miabi resources</dt><dd class="mono small">{{ simResult.resources.apps.join(', ') }}</dd></template>
                </dl>
              </div>
            </div>
          </form>
        </section>
      </div>
    </div>
  </div>
</template>
