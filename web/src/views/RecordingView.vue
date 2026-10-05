<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
// Replays a recorded terminal (asciinema v2) into a read-only xterm: play/pause, speed, seek.
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import type { Terminal } from '@xterm/xterm'
import { api, ApiError, type TerminalSession, type User } from '../api'
import { useCatalog } from '../stores/catalog'
import { useUi } from '../stores/ui'
import { parseCast, inputLines, IDLE_CAP, type Cast } from '../lib/cast'
import { createTerminal, xtermTheme } from '../lib/xterm'
import { fmtDate, duration } from '../lib/format'
import PageHeader from '../components/PageHeader.vue'
import EmptyState from '../components/EmptyState.vue'
import Badge from '../components/Badge.vue'
import Icon from '../components/Icon'

const props = defineProps<{ id: string }>()
const catalog = useCatalog()
const ui = useUi()

const cast = shallowRef<Cast | null>(null)
const meta = ref<TerminalSession | null>(null)
const users = ref<User[]>([])
const error = ref('')
const notFound = ref(false)

const host = ref<HTMLElement | null>(null)
const term = shallowRef<Terminal | null>(null)

const SPEEDS = [1, 2, 4] as const
const speed = ref<number>(1)
const playing = ref(false)
const pos = ref(0)
let idx = 0 // next event to write
let raf = 0
let lastTs = 0

const inputs = computed(() => (cast.value ? inputLines(cast.value.events) : []))
const total = computed(() => cast.value?.duration ?? 0)
const userName = computed(() => {
  const u = users.value.find((x) => x.id === meta.value?.user_id)
  return u ? u.name || u.email : meta.value?.user_id ?? ''
})

function clock(sec: number): string {
  const s = Math.max(0, Math.floor(sec))
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`
}

async function load() {
  try {
    const [text, found, us] = await Promise.all([
      api.terminalRecording(props.id, { quiet: true }),
      api.getTerminal(props.id, { quiet: true }).catch(() => null),
      api.listUsers().catch(() => null),
      catalog.loadAgents(),
    ])
    meta.value = found
    users.value = us ?? []
    cast.value = parseCast(text)
    ui.crumb = `Recording · ${catalog.agentName(meta.value?.agent_id)}`
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound.value = true
    else error.value = e instanceof Error ? e.message : 'The recording could not be loaded.'
    return
  }
  await nextTick()
  mount()
}

function mount() {
  const c = cast.value
  if (!host.value || !c) return
  const { term: t } = createTerminal(ui.resolved, { readonly: true, cols: c.header.width || 100, rows: c.header.height || 30 })
  term.value = t
  t.open(host.value)
  play()
}

function apply(e: Cast['events'][number]) {
  const t = term.value
  if (!t) return
  if (e.kind === 'o') t.write(e.data)
  else if (e.kind === 'r') {
    const m = e.data.match(/^(\d+)x(\d+)$/)
    if (m) t.resize(Math.max(2, +m[1]), Math.max(2, +m[2]))
  }
}

/** Writes every event up to the current position. */
function advance() {
  const c = cast.value
  if (!c) return
  while (idx < c.events.length && c.events[idx].t <= pos.value) apply(c.events[idx++])
}

function tick(ts: number) {
  if (!playing.value) return
  const dt = lastTs ? (ts - lastTs) / 1000 : 0
  lastTs = ts
  pos.value = Math.min(total.value, pos.value + dt * speed.value)
  advance()
  if (pos.value >= total.value) {
    playing.value = false
    return
  }
  raf = requestAnimationFrame(tick)
}

function play() {
  if (!cast.value) return
  if (pos.value >= total.value) seek(0)
  playing.value = true
  lastTs = 0
  cancelAnimationFrame(raf)
  raf = requestAnimationFrame(tick)
}

function pause() {
  playing.value = false
  cancelAnimationFrame(raf)
}

function toggle() {
  if (playing.value) pause()
  else play()
}

/** Jump to the end: everything at once. */
function instant() {
  pause()
  pos.value = total.value
  advance()
}

/** Re-render from the start up to the target time. */
function seek(target: number) {
  const c = cast.value
  const t = term.value
  if (!c || !t) return
  t.reset()
  if (c.header.width && c.header.height) t.resize(c.header.width, c.header.height)
  idx = 0
  pos.value = Math.max(0, Math.min(total.value, target))
  let out = ''
  while (idx < c.events.length && c.events[idx].t <= pos.value) {
    const e = c.events[idx++]
    if (e.kind === 'o') out += e.data
    else if (e.kind === 'r') {
      if (out) t.write(out)
      out = ''
      apply(e)
    }
  }
  if (out) t.write(out)
}

function onSeek(e: Event) {
  const wasPlaying = playing.value
  pause()
  seek(Number((e.target as HTMLInputElement).value))
  if (wasPlaying) play()
}

function jump(t: number) {
  const wasPlaying = playing.value
  pause()
  seek(t)
  if (wasPlaying) play()
}

function onKey(e: KeyboardEvent) {
  if (e.target instanceof HTMLInputElement && e.target.type !== 'range') return
  if (e.key === ' ' || e.key === 'k') {
    e.preventDefault()
    toggle()
  }
}

watch(
  () => ui.resolved,
  (m) => {
    if (term.value) term.value.options.theme = xtermTheme(m)
  },
)

onMounted(() => {
  load()
  window.addEventListener('keydown', onKey)
})
onBeforeUnmount(() => {
  pause()
  window.removeEventListener('keydown', onKey)
  term.value?.dispose()
})

const sessionDuration = computed(() => {
  const m = meta.value
  if (!m?.ended_at) return ''
  return duration(new Date(m.ended_at).getTime() - new Date(m.created_at).getTime())
})
</script>

<template>
  <EmptyState v-if="notFound" title="Recording not found" icon="terminal">
    <template #actions><RouterLink to="/agents" class="btn">Back to agents</RouterLink></template>
  </EmptyState>
  <div v-else class="stack">
    <PageHeader
      title="Terminal recording"
      :back="meta ? { to: `/agents/${meta.agent_id}?tab=terminals`, label: catalog.agentName(meta.agent_id) } : { to: '/agents', label: 'Agents' }"
      style="margin-bottom: 0"
    >
      <template #badges>
        <Badge v-if="meta" :value="meta.status" />
        <span v-if="meta?.truncated" class="badge warn"><Icon name="alert" />truncated</span>
      </template>
      <template #subtitle>
        <template v-if="meta">
          {{ userName }} on <RouterLink :to="`/agents/${meta.agent_id}`">{{ catalog.agentName(meta.agent_id) }}</RouterLink> · {{ fmtDate(meta.created_at) }}
          <template v-if="sessionDuration"> · {{ sessionDuration }}</template>
          <template v-if="meta.ended_at"> · exit {{ meta.exit_code }}</template>
        </template>
        <template v-else>Replay of a recorded terminal session.</template>
      </template>
    </PageHeader>

    <div v-if="error" class="banner danger" role="alert"><Icon name="alert" /><div class="banner-body">{{ error }}</div></div>
    <div v-else-if="!cast" class="skel skel-card" style="height: 420px" aria-busy="true" />

    <div v-show="cast" class="rec-grid">
      <section class="card rec-card" aria-label="Player">
        <div class="rec-controls">
          <button type="button" class="btn btn-sm btn-primary btn-icon" :aria-label="playing ? 'Pause' : 'Play'" :title="playing ? 'Pause (space)' : 'Play (space)'" @click="toggle">
            <Icon :name="playing ? 'pause' : 'play'" />
          </button>
          <span class="num small nowrap">{{ clock(pos) }} / {{ clock(total) }}</span>
          <label for="rec-seek" class="sr-only">Position</label>
          <input id="rec-seek" class="rec-seek" type="range" min="0" :max="total || 0" step="0.05" :value="pos" @input="onSeek" />
          <div class="segmented" role="group" aria-label="Speed">
            <button v-for="s in SPEEDS" :key="s" type="button" :class="{ on: speed === s }" :aria-pressed="speed === s" @click="speed = s">{{ s }}x</button>
            <button type="button" title="Show the end state now" @click="instant">instant</button>
          </div>
        </div>
        <div class="rec-screen">
          <div ref="host" class="rec-host" />
        </div>
        <div v-if="cast?.compressed" class="xs muted" style="padding: 8px 14px">Idle pauses longer than {{ IDLE_CAP }} s are shortened.</div>
      </section>

      <aside class="card rec-side" aria-labelledby="inputs-title">
        <div class="card-head"><h2 id="inputs-title"><Icon name="keyboard" />Keystrokes</h2><span class="small muted">{{ inputs.length }}</span></div>
        <div v-if="!inputs.length" class="empty compact"><p class="small">No input was recorded.</p></div>
        <ol v-else class="rec-inputs">
          <li v-for="(l, i) in inputs" :key="i">
            <button type="button" class="rec-input" :class="{ past: l.t <= pos }" :title="`Jump to ${clock(l.t)}`" @click="jump(l.t)">
              <span class="num xs muted">{{ clock(l.t) }}</span>
              <code class="mono">{{ l.text }}</code>
            </button>
          </li>
        </ol>
      </aside>
    </div>
  </div>
</template>

<style scoped>
.rec-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 280px;
  gap: 16px;
  align-items: start;
}
@media (max-width: 1100px) {
  .rec-grid {
    grid-template-columns: 1fr;
  }
}
.rec-card {
  overflow: hidden;
}
.rec-screen {
  overflow: auto;
  max-height: calc(100dvh - 300px);
  min-height: 240px;
  padding: 10px;
  background: #fff;
  border-top: 1px solid var(--border-primary);
}
[data-theme='dark'] .rec-screen {
  background: #0d0d1a;
}
.rec-host {
  width: max-content;
}
.rec-controls {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 14px;
  flex-wrap: wrap;
}
.rec-seek {
  flex: 1;
  min-width: 140px;
  accent-color: var(--primary-600);
}
.rec-side {
  max-height: 560px;
  display: flex;
  flex-direction: column;
}
.rec-inputs {
  list-style: none;
  margin: 0;
  padding: 6px;
  overflow-y: auto;
}
.rec-input {
  display: flex;
  gap: 8px;
  align-items: baseline;
  width: 100%;
  text-align: left;
  padding: 5px 8px;
  border: 0;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-tertiary);
  cursor: pointer;
  font: inherit;
}
.rec-input:hover {
  background: var(--bg-hover);
}
.rec-input.past {
  color: var(--text-primary);
}
.rec-input code {
  font-size: 12px;
  overflow-wrap: anywhere;
}
</style>
