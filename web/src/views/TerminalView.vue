<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
// A recorded interactive terminal on an agent (admin). One WebSocket per session: text frames carry
// input and resize from the browser and the exit notice from the server; binary frames are output.
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import type { Terminal } from '@xterm/xterm'
import type { FitAddon } from '@xterm/addon-fit'
import { api, ApiError, type Agent } from '../api'
import { useCatalog } from '../stores/catalog'
import { useConfirm } from '../stores/confirm'
import { useUi } from '../stores/ui'
import { createTerminal, debounce, xtermTheme } from '../lib/xterm'
import { useNow } from '../lib/now'
import Badge from '../components/Badge.vue'
import EmptyState from '../components/EmptyState.vue'
import Icon from '../components/Icon'

const props = defineProps<{ id: string }>()
const catalog = useCatalog()
const confirm = useConfirm()
const ui = useUi()
const now = useNow()

type State = 'loading' | 'blocked' | 'connecting' | 'open' | 'exited' | 'closed' | 'failed'
const state = ref<State>('loading')
const agent = ref<Agent | null>(null)
const notFound = ref(false)
const blockedReason = ref('')
const exitInfo = ref<{ code: number; error: string } | null>(null)
const failReason = ref('')
const openedAt = ref(0)
const sessions = ref(0)

const host = ref<HTMLElement | null>(null)
const term = shallowRef<Terminal | null>(null)
let fit: FitAddon | null = null
let ws: WebSocket | null = null
let everOpened = false

const showFrame = computed(() => state.value !== 'loading' && state.value !== 'blocked' && !(state.value === 'failed' && !term.value))
const policy = computed(() => catalog.agentPolicy(agent.value))
const live = computed(() => state.value === 'open')
const elapsed = computed(() => {
  if (!openedAt.value || state.value !== 'open') return ''
  const s = Math.max(0, Math.floor((now.value - openedAt.value) / 1000))
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`
})

function whyBlocked(a: Agent): string {
  if (a.status !== 'online') return `${a.name} is ${a.status}. A terminal needs the agent connected.`
  const p = catalog.agentPolicy(a)
  if (!a.policy_id || !p) return `${a.name} has no policy bound, and only a policy with “terminal” enabled allows terminals.`
  if (!p.document.terminal) return `The ${p.name} policy bound to ${a.name} does not allow terminals. Enable “Terminal” in a policy (built-in policies must be duplicated first) and bind it to this agent.`
  return ''
}

async function init() {
  try {
    const [a] = await Promise.all([api.getAgent(props.id), catalog.loadPolicies()])
    agent.value = a
    ui.crumb = `Terminal · ${a.name}`
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound.value = true
    state.value = 'failed'
    failReason.value = e instanceof Error ? e.message : 'Could not load the agent.'
    return
  }
  const why = whyBlocked(agent.value)
  if (why) {
    blockedReason.value = why
    state.value = 'blocked'
    return
  }
  state.value = 'connecting'
  await nextTick()
  mountTerminal()
  connect()
}

function mountTerminal() {
  if (!host.value || term.value) return
  const t = createTerminal(ui.resolved)
  term.value = t.term
  fit = t.fit
  t.term.open(host.value)
  safeFit()
  t.term.onData((data) => send({ type: 'input', data }))
  t.term.onResize(({ cols, rows }) => send({ type: 'resize', cols, rows }))
}

function safeFit() {
  try {
    fit?.fit()
  } catch {
    /* not laid out yet */
  }
}

function send(msg: Record<string, unknown>) {
  if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify(msg))
}

function connect() {
  const t = term.value
  if (!t) return
  exitInfo.value = null
  failReason.value = ''
  everOpened = false
  state.value = 'connecting'
  sessions.value++
  const sock = new WebSocket(api.terminalURL(props.id, t.cols, t.rows))
  sock.binaryType = 'arraybuffer'
  ws = sock
  sock.onopen = () => {
    if (ws !== sock) return
    everOpened = true
    state.value = 'open'
    openedAt.value = Date.now()
    // The PTY opened at the size in the URL; re-send in case the layout settled since.
    send({ type: 'resize', cols: t.cols, rows: t.rows })
    t.focus()
  }
  sock.onmessage = (ev) => {
    if (ws !== sock) return
    if (typeof ev.data === 'string') {
      try {
        const m = JSON.parse(ev.data) as { type?: string; code?: number; error?: string }
        if (m.type === 'exit') {
          exitInfo.value = { code: m.code ?? 0, error: m.error ?? '' }
          state.value = 'exited'
        }
      } catch {
        /* unknown text frame */
      }
      return
    }
    t.write(new Uint8Array(ev.data as ArrayBuffer))
  }
  sock.onclose = async () => {
    if (ws !== sock) return
    ws = null
    if (state.value === 'exited') return
    if (everOpened) {
      state.value = 'closed'
      return
    }
    // The handshake failed; browsers hide the status, so ask the server why over plain HTTP.
    state.value = 'failed'
    const code = await api.terminalPreflight(props.id)
    failReason.value =
      code === 409
        ? `${agent.value?.name ?? 'The agent'} is not connected.`
        : code === 403
          ? 'Not allowed: the agent’s policy does not allow terminals, or your role is not admin.'
          : code === 401
            ? 'Your session expired. Sign in again.'
            : code === 404
              ? 'The agent no longer exists.'
              : 'The terminal could not be opened (the connection was refused or dropped).'
  }
}

function disconnect() {
  const sock = ws
  ws = null
  sock?.close()
  if (state.value === 'open' || state.value === 'connecting') state.value = 'closed'
}

function reconnect() {
  disconnect()
  const t = term.value
  if (!t) return
  t.reset()
  t.clear()
  connect()
}

// Keep the terminal fitted to its box; the resulting onResize tells the PTY.
const refit = debounce(safeFit, 120)
let ro: ResizeObserver | null = null
onMounted(() => {
  init()
  window.addEventListener('resize', refit)
  if (typeof ResizeObserver !== 'undefined') {
    ro = new ResizeObserver(() => refit())
    watch(host, (el) => el && ro?.observe(el), { immediate: true })
  }
})

watch(
  () => ui.resolved,
  (m) => {
    if (term.value) term.value.options.theme = xtermTheme(m)
  },
)

onBeforeRouteLeave(async () => {
  if (state.value !== 'open') return true
  return confirm.ask({ title: 'Close the terminal?', message: 'Leaving this page ends the shell. The recording up to now is kept.', confirmText: 'Close terminal', danger: true })
})

onBeforeUnmount(() => {
  window.removeEventListener('resize', refit)
  refit.cancel()
  ro?.disconnect()
  disconnect()
  term.value?.dispose()
})
</script>

<template>
  <EmptyState v-if="notFound" title="Agent not found" icon="agents">
    It may have been deleted.
    <template #actions><RouterLink to="/agents" class="btn">Back to agents</RouterLink></template>
  </EmptyState>
  <div v-else class="term-page">
    <header class="term-head">
      <RouterLink :to="`/agents/${id}`" class="btn btn-sm btn-ghost" aria-label="Back to the agent"><Icon name="chevronLeft" />{{ agent?.name ?? 'Agent' }}</RouterLink>
      <div class="term-title">
        <Icon name="terminal" />
        <strong>Terminal</strong>
        <Badge v-if="agent" :value="agent.status" />
        <span v-if="policy" class="small muted hide-mobile">policy {{ policy.name }}</span>
      </div>
      <span class="grow" />
      <span class="term-state small" role="status" :class="state">
        <template v-if="state === 'connecting'"><span class="spinner" style="width: 12px; height: 12px" />Connecting…</template>
        <template v-else-if="state === 'open'"><span class="live-dot open" aria-hidden="true" />Connected<span class="num muted"> · {{ elapsed }}</span></template>
        <template v-else-if="state === 'exited'"><Icon name="power" />Exited</template>
        <template v-else-if="state === 'closed'"><Icon name="x" />Disconnected</template>
        <template v-else-if="state === 'failed'"><Icon name="alert" />Not connected</template>
      </span>
      <button v-if="state === 'open' || state === 'connecting'" type="button" class="btn btn-sm btn-danger-ghost" @click="disconnect"><Icon name="stop" />Disconnect</button>
      <button v-else-if="term" type="button" class="btn btn-sm btn-primary" @click="reconnect"><Icon name="refresh" />{{ sessions ? 'New session' : 'Connect' }}</button>
    </header>

    <div class="term-rec" role="note">
      <Icon name="record" class="rec-dot" />
      <span><strong>Recording</strong> — every keystroke and output is saved to the audit trail.</span>
      <RouterLink :to="{ path: `/agents/${id}`, query: { tab: 'terminals' } }" class="hide-mobile" style="margin-left: auto">Past sessions</RouterLink>
    </div>

    <div v-if="state === 'blocked' || (state === 'failed' && !term)" class="card" style="margin-top: 12px">
      <EmptyState title="The terminal is not available" icon="terminal">
        {{ blockedReason || failReason }}
        <template #actions>
          <RouterLink :to="`/agents/${id}`" class="btn">Back to the agent</RouterLink>
          <RouterLink to="/policies" class="btn">Policies</RouterLink>
        </template>
      </EmptyState>
    </div>
    <div v-else-if="state === 'loading'" class="skel skel-card" style="flex: 1; margin-top: 12px" />

    <div v-show="showFrame" class="term-frame" :class="{ dim: !live && state !== 'connecting' }">
      <div ref="host" class="term-host" aria-label="Terminal" />
      <div v-if="state === 'exited' || state === 'closed' || (state === 'failed' && term)" class="term-overlay" role="alert">
        <div class="term-card">
          <template v-if="state === 'exited'">
            <Icon name="power" :size="22" />
            <strong>The shell exited{{ exitInfo && exitInfo.code >= 0 ? ` with code ${exitInfo.code}` : '' }}</strong>
            <p v-if="exitInfo?.error" class="small">{{ exitInfo.error }}</p>
          </template>
          <template v-else-if="state === 'closed'">
            <Icon name="x" :size="22" />
            <strong>Disconnected</strong>
            <p class="small">The session ended and its recording was saved.</p>
          </template>
          <template v-else>
            <Icon name="alert" :size="22" />
            <strong>Could not connect</strong>
            <p class="small">{{ failReason }}</p>
          </template>
          <div class="row" style="justify-content: center">
            <button type="button" class="btn btn-primary btn-sm" @click="reconnect"><Icon name="refresh" />Open a new session</button>
            <RouterLink :to="{ path: `/agents/${id}`, query: { tab: 'terminals' } }" class="btn btn-sm">Recordings</RouterLink>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.term-page {
  display: flex;
  flex-direction: column;
  height: calc(100dvh - var(--sticky-top) - 48px);
  min-height: 460px;
}
.term-head {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  margin-bottom: 10px;
}
.term-title {
  display: flex;
  align-items: center;
  gap: 8px;
}
.term-state {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: var(--text-secondary);
}
.term-title .icon,
.term-state .icon,
.term-rec .icon {
  width: 15px;
  height: 15px;
}
.term-state.failed {
  color: var(--danger-text);
}
.term-rec {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 7px 12px;
  border-radius: var(--radius);
  background: var(--danger-50);
  border: 1px solid color-mix(in srgb, var(--danger-500) 30%, transparent);
  color: var(--text-primary);
  font-size: 13px;
}
.rec-dot {
  color: var(--danger-600);
  animation: rec 1.6s ease-in-out infinite;
}
@keyframes rec {
  50% {
    opacity: 0.35;
  }
}
@media (prefers-reduced-motion: reduce) {
  .rec-dot {
    animation: none;
  }
}
.term-frame {
  position: relative;
  flex: 1;
  min-height: 280px;
  margin-top: 12px;
  border-radius: var(--radius-lg);
  border: 1px solid var(--border-primary);
  background: var(--term-bg, #fff);
  overflow: hidden;
  padding: 8px 4px 4px 10px;
}
[data-theme='dark'] .term-frame {
  background: #0d0d1a;
}
.term-host {
  width: 100%;
  height: 100%;
}
.term-frame.dim .term-host {
  opacity: 0.55;
}
.term-overlay {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  background: color-mix(in srgb, var(--bg-primary) 35%, transparent);
}
.term-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  text-align: center;
  padding: 18px 22px;
  max-width: 420px;
  border-radius: var(--radius-lg);
  background: var(--bg-primary);
  border: 1px solid var(--border-primary);
  box-shadow: var(--shadow-lg);
}
.term-card p {
  margin: 0 0 6px;
  color: var(--text-secondary);
}
</style>
