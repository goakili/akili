<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed } from 'vue'
import type { Approval, Change, ToolOutcome, ToolRequestPayload } from '../api'
import { duration, safeUrl } from '../lib/format'
import { parseDiff } from '../lib/diff'
import { callSummary, mcpTool, miabiState, parsePlan, toolIcon, type ChangeChild } from '../lib/tools'
import Badge from './Badge.vue'
import JsonBlock from './JsonBlock'
import ApprovalCard from './ApprovalCard.vue'
import DiffViewer from './DiffViewer.vue'
import PlanView from './PlanView.vue'
import Icon, { type IconName } from './Icon'

const props = defineProps<{
  name: string
  input: unknown
  request?: ToolRequestPayload
  result?: ToolOutcome
  approval?: Approval
  /** change_run: the change this call proposed (live status, link). */
  change?: Change
  /** change_run: the calls it ran. */
  children?: ChangeChild[]
  /** A call that belongs to a change, e.g. "change · step 2". */
  tag?: string
}>()
const emit = defineEmits<{ (e: 'approval', a: Approval): void }>()

const icon = computed<IconName>(() => toolIcon(props.name))
const mcp = computed(() => mcpTool(props.name))
const plan = computed(() => (props.name === 'change_run' ? parsePlan(props.input) : null))

const denied = computed(() => props.request?.effect === 'deny' || props.approval?.status === 'denied' || props.approval?.status === 'expired')
const waiting = computed(() => props.approval?.status === 'pending')
const running = computed(() => !props.result && !denied.value && !waiting.value && (props.request?.effect === 'allow' || props.approval?.status === 'approved'))
const decision = computed(() => {
  const r = props.request
  if (!r) return ''
  if (r.effect === 'approve' && props.approval && props.approval.status !== 'pending') return props.approval.status === 'approved' ? 'allow' : 'deny'
  return r.effect
})
const summary = computed(() => callSummary(props.input, props.name))
// git_diff output is a unified diff: render it as one (when it parses; "no changes" stays text).
const isDiff = computed(() => props.name === 'git_diff' && !!props.result && !props.result.is_error && parseDiff(props.result.output).length > 0)
// pr_open / pr_status name the pull request URL in their output.
const prUrl = computed(() => {
  if ((props.name !== 'pr_open' && props.name !== 'pr_status') || !props.result || props.result.is_error) return undefined
  const m = props.result.output.match(/https?:\/\/\S+\/pulls?\/\d+/)
  return safeUrl(m?.[0])
})
const miabi = computed(() => (props.name.startsWith('miabi_') && props.result && !props.result.is_error ? miabiState(props.result.output) : null))
const prMatch = computed(() => props.result?.output.match(/(PR #|MR !)(\d+)/))
const prNumber = computed(() => prMatch.value?.[2])
const prLabel = computed(() => (prMatch.value?.[1] === 'MR !' ? `merge request !${prNumber.value}` : prNumber.value ? `pull request #${prNumber.value}` : 'pull request'))
const hasInput = computed(() => props.input !== null && props.input !== undefined && !(typeof props.input === 'object' && Object.keys(props.input as object).length === 0))
</script>

<template>
  <details class="tool-card" :class="{ denied, waiting, 'tc-plan': !!plan, 'tc-in-change': !!tag }" :open="waiting || undefined">
    <summary>
      <Icon name="chevronRight" class="chev" />
      <span class="tc-icon" aria-hidden="true"><Icon :name="icon" /></span>
      <span v-if="tag" class="badge violet square tc-tag"><Icon name="clipboard" />{{ tag }}</span>
      <span v-if="mcp" class="badge outline square tc-tag" :title="name">MCP · {{ mcp.server }}</span>
      <span class="tc-name">{{ mcp ? mcp.tool : name }}</span>
      <span v-if="plan" class="tc-plan-title truncate">{{ plan.title }}</span>
      <span v-else-if="summary" class="tc-summary truncate">{{ summary }}</span>
      <span class="grow" />
      <Badge v-if="request?.risk && request.risk !== 'unknown'" :value="request.risk" kind="risk" />
      <Badge v-if="change" :value="change.status" :label="`change ${change.status.replace('_', ' ')}`" />
      <Badge v-else-if="decision" :value="decision" kind="effect" />
      <span v-if="running && !change" class="badge accent"><span class="spinner" style="width: 11px; height: 11px" />running</span>
      <Badge v-if="result && !change" :value="result.is_error ? 'error' : 'ok'" />
      <span v-if="result?.duration_ms" class="xs muted num">{{ duration(result.duration_ms) }}</span>
    </summary>
    <div class="tc-body">
      <div v-if="request?.reason && !plan" class="tc-reason"><Icon name="policies" />Policy: {{ request.reason }}</div>
      <div v-if="name === 'lesson_propose'" class="small muted" style="padding-top: 8px">
        A proposed lesson reaches the agent's future sessions only after an operator approves it. <RouterLink to="/lessons">Review lessons</RouterLink>
      </div>
      <template v-if="plan">
        <div class="row between wrap" style="padding-top: 8px">
          <span class="small muted">The agent proposes a change plan. It runs only if a human approves the whole plan.</span>
          <RouterLink v-if="change" :to="`/changes/${change.id}`" class="btn btn-xs"><Icon name="clipboard" />Open change<Icon name="arrowRight" /></RouterLink>
        </div>
        <div v-if="change?.detail" class="banner" :class="change.status === 'succeeded' ? 'ok' : change.status === 'rolled_back' ? 'warn' : 'danger'" role="status">
          <Icon :name="change.status === 'rolled_back' ? 'undo' : 'alert'" /><div class="banner-body">{{ change.detail }}</div>
        </div>
        <PlanView :plan="plan" :risk="change?.risk || request?.risk" />
      </template>
      <div v-else-if="hasInput">
        <div class="tc-label"><span class="section-title">Input</span></div>
        <JsonBlock :value="input" max-height="260px" />
      </div>
      <ApprovalCard v-if="approval" :approval="approval" :show-input="false" :change-id="change?.id" @resolved="emit('approval', $event)" />
      <div v-if="children?.length">
        <div class="tc-label"><span class="section-title">Execution</span><span class="xs muted">{{ children.length }} call{{ children.length === 1 ? '' : 's' }}</span></div>
        <div class="stack tight">
          <ToolCard v-for="c in children" :key="c.key" :name="c.name" :input="c.input" :request="c.request" :result="c.result" :tag="c.tag" />
        </div>
      </div>
      <details v-if="result && plan" class="tc-raw">
        <summary class="small">Agent's execution report</summary>
        <JsonBlock :value="result.output" plain :error="result.is_error" max-height="320px" />
      </details>
      <div v-else-if="result">
        <div class="tc-label">
          <span class="section-title">Output<span v-if="result.truncated" class="muted" style="text-transform: none; font-weight: 400; letter-spacing: 0"> (truncated)</span></span>
        </div>
        <a v-if="prUrl" :href="prUrl" target="_blank" rel="noopener noreferrer" class="btn btn-sm tc-pr">
          <Icon name="gitPR" />Open {{ prLabel }}<Icon name="external" />
        </a>
        <div v-if="miabi" class="row wrap tc-miabi">
          <span v-if="miabi.status" class="badge outline"><Icon name="layers" />{{ miabi.status }}</span>
          <Badge v-if="miabi.health" :value="miabi.health" :label="`health: ${miabi.health}`" />
          <Badge v-if="miabi.traffic" :value="miabi.traffic === 'no-data' ? '' : miabi.traffic" :label="`traffic: ${miabi.traffic}`" />
          <span v-if="miabi.maintenance" class="badge" :class="miabi.maintenance === 'on' ? 'warn' : 'outline'"><Icon name="pause" />maintenance {{ miabi.maintenance }}</span>
        </div>
        <DiffViewer v-if="isDiff" :text="result.output" compact />
        <JsonBlock v-else :value="result.output" plain :error="result.is_error" max-height="320px" />
      </div>
    </div>
  </details>
</template>

<style scoped>
.tc-pr {
  margin-bottom: 8px;
}
.tc-miabi {
  gap: 6px;
  margin-bottom: 8px;
}
.tc-plan-title {
  font-weight: 600;
  font-size: 13px;
  max-width: 380px;
}
.tc-tag {
  font-size: 11px;
}
.tc-raw > summary {
  cursor: pointer;
  color: var(--text-secondary);
  margin-bottom: 6px;
}
.tool-card.tc-plan {
  border-color: color-mix(in srgb, var(--primary-500) 35%, var(--border-primary));
}
</style>
