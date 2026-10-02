<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { api, ApiError, type Approval, type Change, type ChangeCall } from '../api'
import { useCatalog } from '../stores/catalog'
import { useLive } from '../stores/live'
import { useToast } from '../stores/toast'
import { useUi } from '../stores/ui'
import { duration, fmtDate, relTime } from '../lib/format'
import { useNow } from '../lib/now'
import { callArgs, callStatus, checkFailed, PHASES, toolIcon } from '../lib/tools'
import Badge from '../components/Badge.vue'
import ApprovalCard from '../components/ApprovalCard.vue'
import PageHeader from '../components/PageHeader.vue'
import EmptyState from '../components/EmptyState.vue'
import JsonBlock from '../components/JsonBlock'
import Icon, { type IconName } from '../components/Icon'

const props = defineProps<{ id: string }>()
const catalog = useCatalog()
const live = useLive()
const toast = useToast()
const ui = useUi()
const now = useNow()

const change = ref<Change | null>(null)
const approval = ref<Approval | null>(null)
const notFound = ref(false)

function setChange(c: Change) {
  change.value = c
  ui.crumb = c.title
}

async function load() {
  try {
    setChange(await api.getChange(props.id, { quiet: true }))
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound.value = true
    else if (e instanceof ApiError) toast.error(e.message)
    return
  }
  loadApproval()
}

async function loadApproval() {
  const c = change.value
  if (!c?.approval_id) return
  try {
    const d = await api.getSession(c.session_id, { quiet: true })
    approval.value = (d.approvals ?? []).find((a) => a.id === c.approval_id) ?? null
  } catch {
    /* the approval section falls back to the change status */
  }
}

const groups = computed(() => {
  const calls = change.value?.calls ?? []
  return PHASES.map((p) => ({ ...p, calls: calls.filter((c) => c.phase === p.key) }))
})
const hasRollback = computed(() => groups.value[2].calls.length > 0)
const finished = computed(() => ['succeeded', 'failed', 'rolled_back', 'denied', 'expired'].includes(change.value?.status ?? ''))
const elapsed = computed(() => {
  const c = change.value
  if (!c?.started_at) return ''
  const end = c.finished_at ? new Date(c.finished_at).getTime() : now.value
  return duration(Math.max(0, end - new Date(c.started_at).getTime()))
})

const LOOK: Record<string, { icon: IconName; cls: string; label: string }> = {
  pending: { icon: 'circle', cls: 'idle', label: 'not run' },
  running: { icon: 'loader', cls: 'run', label: 'running' },
  ok: { icon: 'checkCircle', cls: 'ok', label: 'ok' },
  failed: { icon: 'xCircle', cls: 'bad', label: 'failed' },
  check_failed: { icon: 'xCircle', cls: 'bad', label: 'check failed' },
  skipped: { icon: 'skip', cls: 'idle', label: 'skipped' },
}
function look(c: ChangeCall) {
  const st = callStatus(c)
  // Calls of a plan that never ran (denied, expired, still pending approval) read "not run".
  if (st === 'pending' && finished.value) return { ...LOOK.skipped, label: 'not run' }
  return LOOK[st] ?? LOOK.pending
}

const banner = computed(() => {
  const c = change.value
  if (!c) return null
  const incomplete = /ROLLBACK INCOMPLETE/i.test(c.detail)
  switch (c.status) {
    case 'pending':
      return { tone: 'warn', icon: 'approvals' as IconName, text: 'Waiting for a human to approve the whole plan. Nothing runs before that.' }
    case 'approved':
      return { tone: 'info', icon: 'checkCircle' as IconName, text: 'Approved. The agent is about to run it.' }
    case 'running':
      return { tone: 'info', icon: 'loader' as IconName, text: 'Running: steps first, then the checks. Outcomes appear below as they land.' }
    case 'succeeded':
      return { tone: 'ok', icon: 'checkCircle' as IconName, text: 'Succeeded: every step ran and every check passed.' }
    case 'rolled_back':
      return { tone: 'warn', icon: 'undo' as IconName, text: `Rolled back${c.detail ? `: ${c.detail.replace(/;\s*rolled back$/i, '')}` : ''}. The rollback succeeded, so the host is back where it started.` }
    case 'failed':
      return {
        tone: 'danger',
        icon: 'alert' as IconName,
        text: `${c.detail || 'The change failed'}.${incomplete || /no rollback/i.test(c.detail) ? ' A human needs to check the host.' : ''}`,
      }
    case 'denied':
      return { tone: '', icon: 'ban' as IconName, text: 'Denied: nothing ran.' }
    case 'expired':
      return { tone: '', icon: 'hourglass' as IconName, text: 'Expired before anyone decided: nothing ran.' }
  }
  return null
})

let off: (() => void) | null = null
let offRe: (() => void) | null = null
onMounted(() => {
  load()
  catalog.loadAgents()
  catalog.loadTools()
  off = live.on((ev) => {
    if (ev.type === 'change.updated' && ev.data && (ev.data as Change).id === props.id) {
      const prev = change.value?.status
      setChange(ev.data as Change)
      if (prev !== (ev.data as Change).status) loadApproval()
    }
    if ((ev.type === 'approval.resolved' || ev.type === 'approval.created') && ev.data && (ev.data as Approval).id === change.value?.approval_id) {
      approval.value = ev.data as Approval
    }
  })
  offRe = live.onReconnect(load)
})
onUnmounted(() => {
  off?.()
  offRe?.()
})
</script>

<template>
  <EmptyState v-if="notFound" title="Change not found" icon="clipboard">
    It may belong to another organization or have been removed.
    <template #actions><RouterLink to="/changes" class="btn">Back to changes</RouterLink></template>
  </EmptyState>
  <div v-else-if="!change" class="stack loose" aria-busy="true">
    <span class="skel lg" style="width: 320px" />
    <div class="skel skel-card" style="height: 200px" />
  </div>
  <div v-else class="stack loose">
    <PageHeader :title="change.title" :back="{ to: '/changes', label: 'Changes' }" style="margin-bottom: 0">
      <template #badges>
        <Badge :value="change.status" />
        <Badge v-if="change.risk" :value="change.risk" kind="risk" :label="`highest risk: ${change.risk}`" />
      </template>
      <template #subtitle>
        Proposed by <RouterLink :to="`/agents/${change.agent_id}`">{{ catalog.agentName(change.agent_id) }}</RouterLink> · {{ relTime(change.created_at, now) }}
      </template>
    </PageHeader>

    <div v-if="banner" class="banner" :class="banner.tone" role="status">
      <Icon :name="banner.icon" :class="{ spin: change.status === 'running' }" />
      <div class="banner-body">{{ banner.text }}</div>
    </div>

    <ApprovalCard v-if="approval && approval.status === 'pending'" :approval="approval" :show-input="false" show-context flat @resolved="approval = $event" />

    <div class="detail-grid">
      <section class="card">
        <div class="card-head"><h2><Icon name="info" />Why</h2></div>
        <div class="card-body stack">
          <div class="pre-wrap">{{ change.reason || '—' }}</div>
          <div v-if="!hasRollback" class="banner warn" role="note">
            <Icon name="alert" />
            <div class="banner-body"><strong>No rollback.</strong> This plan cannot be undone automatically: if a step or check fails, a human has to repair it.</div>
          </div>
        </div>
      </section>
      <section class="card">
        <div class="card-head"><h2><Icon name="clipboard" />Details</h2></div>
        <div class="card-body">
          <dl class="kv">
            <dt>Agent</dt><dd><RouterLink :to="`/agents/${change.agent_id}`">{{ catalog.agentName(change.agent_id) }}</RouterLink></dd>
            <dt>Session</dt><dd><RouterLink :to="`/sessions/${change.session_id}`">Open transcript</RouterLink></dd>
            <template v-if="change.task_id"><dt>Task</dt><dd><RouterLink :to="`/tasks/${change.task_id}`">Open task</RouterLink></dd></template>
            <dt>Approval</dt>
            <dd class="row wrap" style="gap: 6px">
              <Badge v-if="approval" :value="approval.status" />
              <span v-if="approval?.decided_at" class="small muted">{{ relTime(approval.decided_at, now) }}{{ approval.note ? ` · “${approval.note}”` : '' }}</span>
              <RouterLink v-if="approval?.status === 'pending'" to="/approvals" class="small">Approvals inbox</RouterLink>
              <span v-if="!approval" class="muted small mono">{{ change.approval_id || '—' }}</span>
            </dd>
            <dt>Created</dt><dd>{{ fmtDate(change.created_at) }}</dd>
            <dt>Started</dt><dd>{{ fmtDate(change.started_at) }}</dd>
            <dt>Finished</dt><dd>{{ fmtDate(change.finished_at) }}</dd>
            <dt>Duration</dt><dd class="num">{{ elapsed || '—' }}</dd>
            <dt>ID</dt><dd class="mono small">{{ change.id }}</dd>
          </dl>
        </div>
      </section>
    </div>

    <section class="card" aria-labelledby="tl-title">
      <div class="card-head">
        <h2 id="tl-title"><Icon name="list" />Timeline</h2>
        <span class="small muted hide-mobile">Every call is checked against the approved plan; nothing else can run under it.</span>
      </div>
      <div class="card-body stack loose">
        <section v-for="g in groups" :key="g.key" class="phase" :class="`ph-${g.key}`" :aria-label="g.title">
          <h3 class="phase-title">
            <Icon :name="g.icon" />{{ g.title }}<span class="count">{{ g.calls.length }}</span>
            <span class="xs muted phase-help">{{ g.help }}</span>
          </h3>
          <p v-if="!g.calls.length" class="small muted" style="margin: 0 0 0 26px">
            {{ g.key === 'rollback' ? 'This plan has no rollback: failures must be repaired by hand.' : 'None.' }}
          </p>
          <ol v-else class="tl">
            <li v-for="(c, i) in g.calls" :key="`${g.key}-${i}`" class="tl-item" :class="look(c).cls">
              <span class="tl-dot" :title="look(c).label"><Icon :name="look(c).icon" :class="{ spin: c.status === 'running' }" /></span>
              <div class="tl-body">
                <div class="tl-line">
                  <span class="tl-num">{{ i + 1 }}</span>
                  <span class="tl-ic" aria-hidden="true"><Icon :name="toolIcon(c.tool)" /></span>
                  <span class="mono strong">{{ c.tool }}</span>
                  <Badge v-if="c.risk" :value="c.risk" kind="risk" />
                  <span class="grow" />
                  <Badge :value="callStatus(c) === 'pending' && finished ? 'skipped' : callStatus(c)" :label="look(c).label" />
                  <span v-if="c.duration_ms" class="xs muted num">{{ duration(c.duration_ms) }}</span>
                </div>
                <div v-if="c.description" class="tl-desc">{{ c.description }}</div>
                <div v-if="callArgs(c.input).length" class="tl-args">
                  <code v-for="a in callArgs(c.input)" :key="a.key"><span class="muted">{{ a.key }}=</span>{{ a.value }}</code>
                </div>
                <div v-if="c.expect || c.reject" class="tl-checks xs">
                  <span v-if="c.expect" class="ok-t"><Icon name="check" :size="12" />output must contain <code>{{ c.expect }}</code></span>
                  <span v-if="c.reject" class="bad-t"><Icon name="ban" :size="12" />must not contain <code>{{ c.reject }}</code></span>
                </div>
                <div v-if="checkFailed(c)" class="tl-fail small"><Icon name="xCircle" :size="13" />Check failed: {{ checkFailed(c) }}</div>
                <details v-if="c.output" class="tl-out" :open="c.status === 'failed' || !!checkFailed(c) || undefined">
                  <summary class="small">Output</summary>
                  <JsonBlock :value="c.output" plain :error="c.status === 'failed'" max-height="280px" />
                </details>
              </div>
            </li>
          </ol>
        </section>
      </div>
    </section>
  </div>
</template>

<style scoped>
.phase-title {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 0 0 10px;
  font-size: 13px;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--text-secondary);
}
.phase-title .icon {
  width: 15px;
  height: 15px;
}
.phase-title .count {
  font-size: 11px;
  padding: 0 7px;
  border-radius: 999px;
  background: var(--bg-tertiary);
  color: var(--text-tertiary);
}
.phase-help {
  text-transform: none;
  letter-spacing: 0;
  font-weight: 400;
}
.phase.ph-rollback .phase-title {
  color: var(--warning-text);
}
.tl {
  list-style: none;
  margin: 0;
  padding: 0;
  position: relative;
}
.tl-item {
  position: relative;
  display: flex;
  gap: 12px;
  padding-bottom: 14px;
}
.tl-item:not(:last-child)::before {
  content: '';
  position: absolute;
  left: 11px;
  top: 26px;
  bottom: 2px;
  width: 2px;
  background: var(--border-primary);
}
.tl-dot {
  flex: none;
  width: 24px;
  height: 24px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  background: var(--bg-tertiary);
  color: var(--text-muted);
}
.tl-dot .icon {
  width: 15px;
  height: 15px;
}
.ok .tl-dot {
  background: var(--success-50);
  color: var(--success-text);
}
.bad .tl-dot {
  background: var(--danger-50);
  color: var(--danger-text);
}
.run .tl-dot {
  background: var(--primary-50);
  color: var(--primary-text);
}
.tl-body {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 5px;
  padding: 10px 12px;
  border: 1px solid var(--border-primary);
  border-radius: var(--radius);
  background: var(--bg-primary);
}
.bad .tl-body {
  border-color: color-mix(in srgb, var(--danger-500) 40%, var(--border-primary));
}
.tl-line {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.tl-num {
  font-size: 11px;
  font-weight: 600;
  color: var(--text-tertiary);
}
.tl-ic {
  width: 22px;
  height: 22px;
  border-radius: var(--radius-sm);
  display: inline-grid;
  place-items: center;
  background: var(--bg-tertiary);
  color: var(--text-secondary);
}
.tl-ic .icon {
  width: 13px;
  height: 13px;
}
.tl-desc {
  font-size: 13.5px;
}
.tl-args {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
.tl-args code {
  font-family: var(--mono);
  font-size: 11.5px;
  padding: 1px 6px;
  border-radius: var(--radius-sm);
  background: var(--bg-tertiary);
  overflow-wrap: anywhere;
}
.tl-checks {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 14px;
}
.tl-checks span {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}
.tl-checks code {
  font-family: var(--mono);
  color: var(--text-primary);
}
.ok-t {
  color: var(--success-text);
}
.bad-t,
.tl-fail {
  color: var(--danger-text);
}
.tl-fail {
  display: flex;
  align-items: center;
  gap: 6px;
  font-weight: 600;
}
.tl-out > summary {
  cursor: pointer;
  color: var(--text-secondary);
  margin-bottom: 6px;
}
.spin {
  animation: spin 1s linear infinite;
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
