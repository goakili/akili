<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { api, ApiError, type ChatChannel, type ChatChannelInput, type ChatIdentity, type ChatKind, type LinkCode, type TestResult, type User } from '../api'
import { useAuth } from '../stores/auth'
import { useCatalog } from '../stores/catalog'
import { useConfirm } from '../stores/confirm'
import { useToast } from '../stores/toast'
import { countdown, fmtDate, relTime } from '../lib/format'
import { useNow } from '../lib/now'
import Badge from '../components/Badge.vue'
import Modal from '../components/Modal.vue'
import CopyField from '../components/CopyField.vue'
import PageHeader from '../components/PageHeader.vue'
import EmptyState from '../components/EmptyState.vue'
import SkeletonRows from '../components/SkeletonRows.vue'
import Icon, { type IconName } from '../components/Icon'

const auth = useAuth()
const catalog = useCatalog()
const confirm = useConfirm()
const toast = useToast()
const now = useNow()

const KINDS: { kind: ChatKind; label: string; icon: IconName; defaultBase: string }[] = [
  { kind: 'telegram', label: 'Telegram', icon: 'send', defaultBase: 'https://api.telegram.org' },
  { kind: 'slack', label: 'Slack', icon: 'hash', defaultBase: 'https://slack.com/api' },
  { kind: 'signal', label: 'Signal', icon: 'phone', defaultBase: '' },
]
const kindOf = (k: ChatKind) => KINDS.find((x) => x.kind === k) ?? KINDS[0]

const COMMANDS: { cmd: string; help: string }[] = [
  { cmd: '/help', help: 'List the commands.' },
  { cmd: '/agents', help: 'The agents you can talk to.' },
  { cmd: '/agent <name>', help: 'Switch this conversation to another agent.' },
  { cmd: '/new', help: 'Start a fresh session with the current agent.' },
  { cmd: '/task <goal>', help: 'Create a task for the current agent.' },
  { cmd: '/status', help: 'The current agent, session and running tasks.' },
  { cmd: '/approvals', help: 'Approvals waiting for you.' },
  { cmd: '/approve <id>', help: 'Approve a pending tool call or change plan.' },
  { cmd: '/deny <id>', help: 'Deny it.' },
  { cmd: '/unlink', help: 'Unlink this chat account from Akili.' },
]

const SLACK_EVENTS = ['app_mention', 'message.im']
const SLACK_SCOPES = ['chat:write', 'app_mentions:read', 'im:history']

const channels = ref<ChatChannel[]>([])
const loading = ref(true)
const busy = ref<string | null>(null)
const tests = ref<Record<string, TestResult | 'running'>>({})

async function loadChannels() {
  try {
    channels.value = (await api.listChatChannels()) ?? []
  } catch {
    /* toasted */
  } finally {
    loading.value = false
  }
}

const channelName = (id: string) => channels.value.find((c) => c.id === id)?.name ?? 'deleted channel'
const channelKind = (id: string) => channels.value.find((c) => c.id === id)?.kind
const agents = computed(() => catalog.agents.filter((a) => a.status !== 'revoked'))


interface Form {
  name: string
  kind: ChatKind
  enabled: boolean
  api_base_url: string
  account: string
  token: string
  signing_secret: string
  default_agent_id: string
}

const show = ref(false)
const editing = ref<ChatChannel | null>(null)
/** After creating a Slack channel: the saved channel, shown with its request URLs. */
const saved = ref<ChatChannel | null>(null)
const saving = ref(false)
const tried = ref(false)
const formError = ref('')

const blank = (): Form => ({ name: '', kind: 'telegram', enabled: true, api_base_url: '', account: '', token: '', signing_secret: '', default_agent_id: '' })
const form = ref<Form>(blank())

function openNew() {
  editing.value = null
  saved.value = null
  form.value = blank()
  tried.value = false
  formError.value = ''
  show.value = true
}

function openEdit(c: ChatChannel) {
  editing.value = c
  saved.value = null
  form.value = { ...blank(), name: c.name, kind: c.kind, enabled: c.enabled, api_base_url: c.api_base_url, account: c.account, default_agent_id: c.default_agent_id ?? '' }
  tried.value = false
  formError.value = ''
  show.value = true
}

const hasToken = computed(() => !!editing.value?.has_token)
const hasSecret = computed(() => !!editing.value?.has_secret)
const isUrl = (s: string) => /^https?:\/\//i.test(s.trim())

const errors = computed(() => {
  const f = form.value
  const e: Record<string, string> = {}
  if (!f.name.trim()) e.name = 'Give the channel a name.'
  if (f.kind === 'signal') {
    if (!f.api_base_url.trim()) e.api_base_url = 'Enter the signal-cli REST API URL, e.g. http://signal-api:8080.'
    if (!f.account.trim()) e.account = "Enter the bot's phone number, e.g. +15551234567."
  } else {
    if (!f.token.trim() && !hasToken.value) e.token = f.kind === 'slack' ? 'Paste the bot token (xoxb-…).' : 'Paste the bot token from @BotFather.'
    if (f.kind === 'slack' && !f.signing_secret.trim() && !hasSecret.value) e.signing_secret = "Paste the app's signing secret."
  }
  if (f.api_base_url.trim() && !isUrl(f.api_base_url)) e.api_base_url = 'Use a full http(s):// URL.'
  return e
})
const err = (k: string) => (tried.value ? errors.value[k] : '')

function inputFrom(f: Form): ChatChannelInput {
  const body: ChatChannelInput = {
    name: f.name.trim(),
    kind: f.kind,
    enabled: f.enabled,
    api_base_url: f.api_base_url.trim(),
    default_agent_id: f.default_agent_id || null,
  }
  if (f.kind === 'signal') body.account = f.account.trim()
  if (f.kind !== 'signal' && f.token.trim()) body.token = f.token.trim()
  if (f.kind === 'slack' && f.signing_secret.trim()) body.signing_secret = f.signing_secret.trim()
  return body
}

async function save() {
  tried.value = true
  formError.value = ''
  if (Object.keys(errors.value).length) return
  saving.value = true
  const body = inputFrom(form.value)
  try {
    if (editing.value) {
      replace(await api.updateChatChannel(editing.value.id, body))
      show.value = false
      toast.success(`Channel ${body.name} updated`)
    } else {
      const c = await api.createChatChannel(body)
      channels.value = [...channels.value, c].sort((a, b) => a.name.localeCompare(b.name))
      toast.success(`Channel ${body.name} added`)
      if (c.kind === 'slack') saved.value = c
      else show.value = false
    }
  } catch (e) {
    formError.value = e instanceof ApiError ? e.message : 'Saving failed.'
  } finally {
    saving.value = false
  }
}

function replace(c: ChatChannel) {
  const i = channels.value.findIndex((x) => x.id === c.id)
  if (i >= 0) channels.value[i] = c
}

async function toggle(c: ChatChannel) {
  busy.value = c.id
  try {
    replace(
      await api.updateChatChannel(c.id, {
        name: c.name,
        kind: c.kind,
        enabled: !c.enabled,
        api_base_url: c.api_base_url,
        account: c.account,
        default_agent_id: c.default_agent_id,
      }),
    )
    toast.success(c.enabled ? `${c.name} disabled: the bot stops answering` : `${c.name} enabled`)
  } catch {
    /* toasted */
  } finally {
    busy.value = null
  }
}

async function test(c: ChatChannel) {
  tests.value[c.id] = 'running'
  try {
    tests.value[c.id] = await api.testChatChannel(c.id)
  } catch {
    delete tests.value[c.id]
  }
}
const testOf = (id: string) => tests.value[id]
const done = (id: string): TestResult | null => {
  const t = tests.value[id]
  return t && t !== 'running' ? t : null
}

async function remove(c: ChatChannel) {
  const ok = await confirm.ask({
    title: `Delete channel ${c.name}?`,
    message: 'The bot stops answering, its stored credentials are discarded and every chat account linked through it is unlinked. Sessions and tasks it created are kept.',
    confirmText: `Delete ${c.name}`,
    danger: true,
  })
  if (!ok) return
  busy.value = c.id
  try {
    await api.deleteChatChannel(c.id)
    channels.value = channels.value.filter((x) => x.id !== c.id)
    identities.value = identities.value.filter((x) => x.channel_id !== c.id)
    toast.success('Channel deleted')
  } catch {
    /* toasted */
  } finally {
    busy.value = null
  }
}

const dialogTitle = computed(() => (saved.value ? 'Slack channel added' : editing.value ? `Edit ${editing.value.name}` : 'Add chat channel'))
/** The Slack channel whose request URLs the dialog shows. */
const slackUrls = computed(() => {
  const c = saved.value ?? editing.value
  return c?.kind === 'slack' ? c : null
})


const identities = ref<ChatIdentity[]>([])
const identitiesLoading = ref(true)
const users = ref<User[]>([])
const link = ref<LinkCode | null>(null)
const linking = ref(false)
let poll: ReturnType<typeof setInterval> | null = null

async function loadIdentities(quiet = false) {
  try {
    identities.value = (await api.listChatIdentities(quiet ? { quiet: true } : undefined)) ?? []
  } catch {
    /* toasted */
  } finally {
    identitiesLoading.value = false
  }
}

const linkLeft = computed(() => (link.value ? countdown(link.value.expires_at, now.value) : ''))
const linkExpired = computed(() => linkLeft.value === 'expired')

async function getLinkCode() {
  linking.value = true
  try {
    link.value = await api.chatLinkCode()
    startPoll()
  } catch {
    /* toasted */
  } finally {
    linking.value = false
  }
}

// While a code is live, watch for the new account so the page confirms the link without a reload.
function startPoll() {
  stopPoll()
  const mine = new Set(identities.value.filter((i) => i.user_id === auth.user?.id).map((i) => i.id))
  poll = setInterval(async () => {
    if (!link.value || linkExpired.value) return stopPoll()
    await loadIdentities(true)
    const fresh = identities.value.find((i) => i.user_id === auth.user?.id && !mine.has(i.id))
    if (fresh) {
      toast.success(`Linked ${fresh.display_name || fresh.external_id} on ${channelName(fresh.channel_id)}`)
      link.value = null
      stopPoll()
    }
  }, 4000)
}
function stopPoll() {
  if (poll) clearInterval(poll)
  poll = null
}

function userLabel(id: string): string {
  if (id === auth.user?.id) return 'You'
  const u = users.value.find((x) => x.id === id)
  return u ? u.name || u.email : id
}

async function unlink(i: ChatIdentity) {
  const mine = i.user_id === auth.user?.id
  const ok = await confirm.ask({
    title: `Unlink ${i.display_name || i.external_id}?`,
    message: mine
      ? `The bot on ${channelName(i.channel_id)} stops acting for you until you link again.`
      : `The bot on ${channelName(i.channel_id)} stops acting for ${userLabel(i.user_id)} until they link again.`,
    confirmText: 'Unlink',
    danger: true,
  })
  if (!ok) return
  busy.value = i.id
  try {
    await api.deleteChatIdentity(i.id)
    identities.value = identities.value.filter((x) => x.id !== i.id)
    toast.success('Chat account unlinked')
  } catch {
    /* toasted */
  } finally {
    busy.value = null
  }
}

onMounted(() => {
  loadChannels()
  loadIdentities()
  catalog.loadAgents()
  if (auth.isAdmin) api.listUsers().then((u) => (users.value = u ?? [])).catch(() => {})
})
onUnmounted(stopPoll)
</script>

<template>
  <div>
    <PageHeader title="Chat" subtitle="Talk to agents from Slack, Telegram or Signal. Each person links their chat account to their Akili user, so every message runs with their role and is audited.">
      <button v-if="auth.isAdmin" type="button" class="btn btn-primary" @click="openNew"><Icon name="plus" />Add channel</button>
    </PageHeader>

    <section class="card" style="margin-bottom: 20px">
      <div class="card-head">
        <h2><Icon name="messages" />Channels</h2>
        <span v-if="!auth.isAdmin" class="badge outline"><Icon name="lock" />Read-only (admin required)</span>
      </div>
      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr>
              <th>Channel</th><th>Enabled</th><th class="hide-mobile">Default agent</th><th>Status</th>
              <th v-if="auth.isAdmin"><span class="sr-only">Actions</span></th>
            </tr>
          </thead>
          <tbody>
            <SkeletonRows v-if="loading" :cols="auth.isAdmin ? 5 : 4" :rows="2" />
            <tr v-else-if="!channels.length">
              <td :colspan="auth.isAdmin ? 5 : 4">
                <EmptyState title="No chat channels yet" icon="messages">
                  Connect a Telegram, Slack or Signal bot so people can chat with agents, start tasks and decide approvals from their phone. Bot credentials stay on the control plane.
                  <template v-if="auth.isAdmin" #actions><button type="button" class="btn btn-primary" @click="openNew"><Icon name="plus" />Add channel</button></template>
                </EmptyState>
              </td>
            </tr>
            <tr v-for="c in loading ? [] : channels" :key="c.id">
              <td>
                <span class="row" style="gap: 8px">
                  <Icon :name="kindOf(c.kind).icon" :size="16" />
                  <span>
                    <span class="cell-title">{{ c.name }}</span>
                    <span class="cell-sub" style="display: block">{{ kindOf(c.kind).label }}<template v-if="c.kind === 'signal' && c.account"> · <span class="mono">{{ c.account }}</span></template></span>
                  </span>
                </span>
              </td>
              <td>
                <label v-if="auth.isAdmin" class="switch">
                  <input type="checkbox" :checked="c.enabled" :disabled="busy === c.id" :aria-label="`Enable ${c.name}`" @change="toggle(c)" />
                  <span class="sr-only">{{ c.enabled ? 'enabled' : 'disabled' }}</span>
                </label>
                <Badge v-else :value="c.enabled ? 'enabled' : 'disabled'" />
              </td>
              <td class="hide-mobile">
                <span v-if="c.default_agent_id">{{ catalog.agentName(c.default_agent_id) }}</span>
                <span v-else class="small muted">none: users pick with <code>/agent</code></span>
              </td>
              <td style="max-width: 320px">
                <span v-if="testOf(c.id) === 'running'" class="row small muted"><span class="spinner" />testing…</span>
                <template v-else-if="done(c.id)">
                  <span class="badge" :class="done(c.id)!.ok ? 'ok' : 'danger'"><Icon :name="done(c.id)!.ok ? 'checkCircle' : 'xCircle'" />{{ done(c.id)!.ok ? `ok · ${done(c.id)!.latency_ms} ms` : 'failed' }}</span>
                  <div class="cell-sub" :class="{ 'danger-text': !done(c.id)!.ok }" style="white-space: normal; overflow-wrap: anywhere">{{ done(c.id)!.ok ? done(c.id)!.reply : done(c.id)!.error }}</div>
                </template>
                <template v-else>
                  <div v-if="c.last_error" class="small danger-text" style="overflow-wrap: anywhere"><Icon name="alert" :size="13" style="vertical-align: -2px" /> {{ c.last_error }}</div>
                  <div class="cell-sub" :title="fmtDate(c.last_seen_at)">last seen {{ relTime(c.last_seen_at, now) }}</div>
                </template>
              </td>
              <td v-if="auth.isAdmin" class="right nowrap">
                <div class="row end" style="gap: 4px">
                  <button type="button" class="btn btn-sm" :disabled="testOf(c.id) === 'running'" @click="test(c)"><Icon name="zap" />Test</button>
                  <button type="button" class="btn btn-sm btn-ghost btn-icon" :aria-label="`Edit ${c.name}`" title="Edit" :disabled="busy === c.id" @click="openEdit(c)"><Icon name="edit" /></button>
                  <button type="button" class="btn btn-sm btn-ghost btn-icon btn-danger-ghost" :aria-label="`Delete ${c.name}`" title="Delete" :disabled="busy === c.id" @click="remove(c)"><Icon name="trash" /></button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <div class="chat-grid">
      <section class="card">
        <div class="card-head">
          <h2><Icon name="link" />{{ auth.isAdmin ? 'Linked chat accounts' : 'My chat accounts' }}</h2>
          <button type="button" class="btn btn-sm btn-primary" :disabled="linking || !channels.some((c) => c.enabled)" @click="getLinkCode">
            <span v-if="linking" class="spinner" /><Icon v-else name="link" />{{ link && !linkExpired ? 'New code' : 'Link my account' }}
          </button>
        </div>
        <div v-if="link" class="card-body stack" style="border-bottom: 1px solid var(--border-primary)">
          <template v-if="!linkExpired">
            <div class="link-code mono" aria-label="Link code">{{ link.code }}</div>
            <CopyField :value="`/link ${link.code}`" label="Send this to the bot" />
            <p class="small muted" style="margin: 0">
              Send <code>/link {{ link.code }}</code> to the bot (Telegram, Signal) or mention it in Slack within 10 minutes.
              Expires in <strong class="num">{{ linkLeft }}</strong>. This page updates when the link succeeds.
            </p>
          </template>
          <div v-else class="row between wrap">
            <span class="small muted"><Icon name="hourglass" :size="14" style="vertical-align: -2px" /> The code expired.</span>
            <button type="button" class="btn btn-sm" @click="getLinkCode"><Icon name="refresh" />Get a new code</button>
          </div>
        </div>
        <div class="table-wrap">
          <table class="table">
            <thead>
              <tr>
                <th>Account</th><th>Channel</th><th v-if="auth.isAdmin">User</th><th class="hide-mobile">Last used</th>
                <th><span class="sr-only">Actions</span></th>
              </tr>
            </thead>
            <tbody>
              <SkeletonRows v-if="identitiesLoading" :cols="auth.isAdmin ? 5 : 4" :rows="2" />
              <tr v-else-if="!identities.length">
                <td :colspan="auth.isAdmin ? 5 : 4">
                  <EmptyState title="No linked accounts" icon="link" compact>
                    {{ channels.some((c) => c.enabled) ? 'Link your chat account to talk to agents from Slack, Telegram or Signal.' : 'An admin needs to add and enable a chat channel first.' }}
                  </EmptyState>
                </td>
              </tr>
              <tr v-for="i in identitiesLoading ? [] : identities" :key="i.id">
                <td>
                  <div class="cell-title">{{ i.display_name || i.external_id }}</div>
                  <div v-if="i.display_name" class="cell-sub mono">{{ i.external_id }}</div>
                </td>
                <td class="nowrap">
                  <span class="row" style="gap: 6px"><Icon v-if="channelKind(i.channel_id)" :name="kindOf(channelKind(i.channel_id)!).icon" :size="15" />{{ channelName(i.channel_id) }}</span>
                </td>
                <td v-if="auth.isAdmin" class="nowrap" :class="{ strong: i.user_id === auth.user?.id }">{{ userLabel(i.user_id) }}</td>
                <td class="nowrap hide-mobile" :title="`Linked ${fmtDate(i.created_at)}`">{{ relTime(i.last_used_at, now) }}</td>
                <td class="right nowrap">
                  <button type="button" class="btn btn-sm btn-ghost btn-danger-ghost" :disabled="busy === i.id" @click="unlink(i)"><Icon name="x" />Unlink</button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section class="card">
        <div class="card-head"><h2><Icon name="info" />Bot commands</h2></div>
        <div class="card-body">
          <dl class="kv">
            <template v-for="c in COMMANDS" :key="c.cmd">
              <dt><code class="mono">{{ c.cmd }}</code></dt>
              <dd class="small">{{ c.help }}</dd>
            </template>
          </dl>
          <p class="xs muted" style="margin: 12px 0 0">Anything else is sent to the current agent as a chat message. Risky tool calls still wait for an approval.</p>
        </div>
      </section>
    </div>

    <Modal :open="show" :title="dialogTitle" wide :dismissable="!saving" @close="show = false">
      <div v-if="saved" class="stack">
        <div class="banner ok"><Icon name="checkCircle" /><div class="banner-body"><strong>{{ saved.name }}</strong> is saved. Finish the setup in your Slack app, then test it.</div></div>
      </div>

      <form v-else id="chat-channel-form" class="stack" novalidate @submit.prevent="save">
        <div v-if="formError" class="banner danger" role="alert"><Icon name="alert" /><div class="banner-body">{{ formError }}</div></div>
        <div class="field">
          <span id="cc-kind" class="label">Kind</span>
          <div class="segmented" role="radiogroup" aria-labelledby="cc-kind" style="align-self: flex-start">
            <button v-for="k in KINDS" :key="k.kind" type="button" role="radio" :aria-checked="form.kind === k.kind" :class="{ on: form.kind === k.kind }" :disabled="!!editing" @click="form.kind = k.kind"><Icon :name="k.icon" />{{ k.label }}</button>
          </div>
          <span v-if="editing" class="hint">The kind of an existing channel cannot change; add a new one instead.</span>
          <span v-else-if="form.kind === 'telegram'" class="hint">The control plane polls Telegram for messages, so no public URL is needed.</span>
          <span v-else-if="form.kind === 'slack'" class="hint">Slack sends events to the control plane, so it must be reachable from the internet. The request URLs are shown after saving.</span>
          <span v-else class="hint">Uses a <a href="https://github.com/bbernhard/signal-cli-rest-api" target="_blank" rel="noopener noreferrer">signal-cli REST API</a> server registered with the bot's number.</span>
        </div>

        <div class="grid-2">
          <div class="field">
            <label for="cc-name">Name <span class="req" aria-hidden="true">*</span></label>
            <input id="cc-name" v-model="form.name" class="input" :class="{ invalid: err('name') }" required maxlength="120" :placeholder="`e.g. ops-${form.kind}`" />
            <span v-if="err('name')" class="error-msg"><Icon name="alert" />{{ err('name') }}</span>
          </div>
          <label class="switch" style="align-self: end; padding-bottom: 8px"><input v-model="form.enabled" type="checkbox" />Enabled: the bot answers</label>

          <template v-if="form.kind === 'signal'">
            <div class="field">
              <label for="cc-base">REST API URL <span class="req" aria-hidden="true">*</span></label>
              <input id="cc-base" v-model="form.api_base_url" class="input mono" :class="{ invalid: err('api_base_url') }" required placeholder="http://signal-api:8080" spellcheck="false" />
              <span v-if="err('api_base_url')" class="error-msg"><Icon name="alert" />{{ err('api_base_url') }}</span>
              <span v-else class="hint">The signal-cli REST server, reachable from the control plane.</span>
            </div>
            <div class="field">
              <label for="cc-account">Bot number <span class="req" aria-hidden="true">*</span></label>
              <input id="cc-account" v-model="form.account" class="input mono" :class="{ invalid: err('account') }" required inputmode="tel" autocomplete="off" placeholder="+15551234567" />
              <span v-if="err('account')" class="error-msg"><Icon name="alert" />{{ err('account') }}</span>
              <span v-else class="hint">The number registered in signal-cli, in international format.</span>
            </div>
          </template>

          <template v-else>
            <div class="field span-all">
              <label for="cc-token">Bot token <span v-if="!hasToken" class="req" aria-hidden="true">*</span></label>
              <input
                id="cc-token"
                v-model="form.token"
                class="input mono"
                :class="{ invalid: err('token') }"
                type="password"
                autocomplete="new-password"
                :placeholder="hasToken ? '•••• set — leave empty to keep' : form.kind === 'slack' ? 'xoxb-…' : '123456789:AA…'"
              />
              <span v-if="err('token')" class="error-msg"><Icon name="alert" />{{ err('token') }}</span>
              <span v-else class="hint">Write-only.
                <template v-if="form.kind === 'telegram'">Create a bot with <strong>@BotFather</strong> (<code>/newbot</code>) and paste the token it gives you.</template>
                <template v-else>The <strong>Bot User OAuth Token</strong> from your Slack app's OAuth &amp; Permissions page.</template>
              </span>
            </div>
            <div v-if="form.kind === 'slack'" class="field span-all">
              <label for="cc-secret">Signing secret <span v-if="!hasSecret" class="req" aria-hidden="true">*</span></label>
              <input
                id="cc-secret"
                v-model="form.signing_secret"
                class="input mono"
                :class="{ invalid: err('signing_secret') }"
                type="password"
                autocomplete="new-password"
                :placeholder="hasSecret ? '•••• set — leave empty to keep' : ''"
              />
              <span v-if="err('signing_secret')" class="error-msg"><Icon name="alert" />{{ err('signing_secret') }}</span>
              <span v-else class="hint">Write-only. From Basic Information → App Credentials. Akili rejects requests Slack did not sign.</span>
            </div>
          </template>

          <div class="field span-all">
            <label for="cc-agent">Default agent <span class="opt">(optional)</span></label>
            <select id="cc-agent" v-model="form.default_agent_id" class="select">
              <option value="">None: users pick one with /agent</option>
              <option v-for="a in agents" :key="a.id" :value="a.id">{{ a.name }} · {{ a.status }}</option>
            </select>
            <span class="hint">New conversations start with this agent.</span>
          </div>
        </div>

        <details v-if="form.kind !== 'signal'" class="rule-block" :open="!!form.api_base_url || !!err('api_base_url') || undefined">
          <summary class="strong" style="cursor: pointer">Advanced</summary>
          <div class="field" style="margin-top: 14px">
            <label for="cc-base">API base URL <span class="opt">(optional)</span></label>
            <input id="cc-base" v-model="form.api_base_url" class="input mono" :class="{ invalid: err('api_base_url') }" :placeholder="kindOf(form.kind).defaultBase" spellcheck="false" />
            <span v-if="err('api_base_url')" class="error-msg"><Icon name="alert" />{{ err('api_base_url') }}</span>
            <span v-else class="hint">Leave empty for {{ kindOf(form.kind).label }}'s API. Set it for a proxy or a self-hosted Bot API server.</span>
          </div>
        </details>
      </form>

      <template v-if="slackUrls && (saved || editing)">
        <div class="section-title" style="margin-top: 16px">Slack app setup</div>
        <div class="stack">
          <CopyField v-if="slackUrls.events_url" :value="slackUrls.events_url" label="Events request URL" />
          <CopyField v-if="slackUrls.interact_url" :value="slackUrls.interact_url" label="Interactivity request URL" />
          <ol class="small setup-steps">
            <li><strong>Event Subscriptions:</strong> enable events, set the request URL to the events URL above, and subscribe the bot to <template v-for="(ev, i) in SLACK_EVENTS" :key="ev"><code>{{ ev }}</code>{{ i < SLACK_EVENTS.length - 1 ? ' and ' : '' }}</template>.</li>
            <li><strong>Interactivity &amp; Shortcuts:</strong> turn it on and set the request URL to the interactivity URL above (approval buttons use it).</li>
            <li><strong>OAuth &amp; Permissions:</strong> add the bot scopes <template v-for="(s, i) in SLACK_SCOPES" :key="s"><code>{{ s }}</code>{{ i < SLACK_SCOPES.length - 1 ? ', ' : '' }}</template>, then reinstall the app to the workspace.</li>
          </ol>
        </div>
      </template>

      <template #footer>
        <template v-if="saved">
          <div class="row" style="margin-right: auto; gap: 10px; min-width: 0">
            <button type="button" class="btn" :disabled="testOf(saved.id) === 'running'" @click="test(saved)">
              <span v-if="testOf(saved.id) === 'running'" class="spinner" /><Icon v-else name="zap" />Test
            </button>
            <span v-if="done(saved.id)" class="small truncate" :class="done(saved.id)!.ok ? 'ok-text' : 'danger-text'" :title="done(saved.id)!.error">
              {{ done(saved.id)!.ok ? done(saved.id)!.reply : done(saved.id)!.error }}
            </span>
          </div>
          <button type="button" class="btn btn-primary" @click="show = false">Done</button>
        </template>
        <template v-else>
          <div v-if="editing" class="row" style="margin-right: auto; gap: 10px; min-width: 0">
            <button type="button" class="btn" :disabled="testOf(editing.id) === 'running'" @click="test(editing)">
              <span v-if="testOf(editing.id) === 'running'" class="spinner" /><Icon v-else name="zap" />Test
            </button>
            <span v-if="done(editing.id)" class="small truncate" :class="done(editing.id)!.ok ? 'ok-text' : 'danger-text'" :title="done(editing.id)!.error">
              {{ done(editing.id)!.ok ? done(editing.id)!.reply : done(editing.id)!.error }}
            </span>
          </div>
          <button type="button" class="btn" @click="show = false">Cancel</button>
          <button type="submit" form="chat-channel-form" class="btn btn-primary" :disabled="saving">
            <span v-if="saving" class="spinner" />{{ editing ? 'Save' : 'Add channel' }}
          </button>
        </template>
      </template>
    </Modal>
  </div>
</template>

<style scoped>
.chat-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.6fr) minmax(0, 1fr);
  gap: 20px;
  align-items: start;
}
@media (max-width: 1000px) {
  .chat-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
.link-code {
  font-size: 32px;
  font-weight: 700;
  letter-spacing: 0.12em;
  color: var(--primary-text);
  text-align: center;
  padding: 6px 0;
  overflow-wrap: anywhere;
}
.setup-steps {
  margin: 0;
  padding-left: 20px;
  display: grid;
  gap: 6px;
}
</style>
