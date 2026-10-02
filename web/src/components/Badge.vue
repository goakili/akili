<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed } from 'vue'
import Icon, { type IconName } from './Icon'

// One badge for every status-like value in the API, so colour, icon and wording stay consistent across
// pages. Never colour alone: every tone carries an icon and a word.
const props = defineProps<{ value: string | null | undefined; kind?: 'risk' | 'effect' | 'status'; label?: string; noIcon?: boolean }>()

type Tone = '' | 'ok' | 'warn' | 'danger' | 'info' | 'accent' | 'violet'
interface Look {
  tone: Tone
  icon: IconName
  anim?: 'spin' | 'pulse'
  text?: string
}

const STATUS: Record<string, Look> = {
  // agents
  online: { tone: 'ok', icon: 'circleDot' },
  offline: { tone: '', icon: 'circle' },
  pending: { tone: 'warn', icon: 'hourglass' },
  revoked: { tone: 'danger', icon: 'ban' },
  draining: { tone: 'warn', icon: 'pause' },
  // tasks
  queued: { tone: 'info', icon: 'clock' },
  assigned: { tone: 'accent', icon: 'loader', anim: 'spin' },
  running: { tone: 'accent', icon: 'loader', anim: 'spin' },
  succeeded: { tone: 'ok', icon: 'checkCircle' },
  failed: { tone: 'danger', icon: 'xCircle' },
  cancelled: { tone: '', icon: 'ban' },
  timed_out: { tone: 'danger', icon: 'hourglass', text: 'timed out' },
  needs_approval: { tone: 'warn', icon: 'approvals', text: 'needs approval' },
  // approvals
  approved: { tone: 'ok', icon: 'checkCircle' },
  denied: { tone: 'danger', icon: 'xCircle' },
  expired: { tone: '', icon: 'hourglass' },
  // sessions
  open: { tone: 'ok', icon: 'circleDot' },
  closed: { tone: '', icon: 'lock' },
  idle: { tone: '', icon: 'circle' },
  thinking: { tone: 'accent', icon: 'brain', anim: 'pulse' },
  running_tool: { tone: 'violet', icon: 'wrench', anim: 'pulse', text: 'running tool' },
  waiting_approval: { tone: 'warn', icon: 'approvals', anim: 'pulse', text: 'waiting for approval' },
  chat: { tone: 'info', icon: 'chat' },
  task: { tone: 'violet', icon: 'tasks' },
  // results
  ok: { tone: 'ok', icon: 'check' },
  error: { tone: 'danger', icon: 'x' },
  // keys / generic
  active: { tone: 'ok', icon: 'checkCircle' },
  default: { tone: 'accent', icon: 'zap' },
  builtin: { tone: 'info', icon: 'lock', text: 'built-in' },
  // change plans
  rolled_back: { tone: 'warn', icon: 'undo', text: 'rolled back' },
  skipped: { tone: '', icon: 'skip' },
  check_failed: { tone: 'danger', icon: 'xCircle', text: 'check failed' },
  runbook: { tone: 'violet', icon: 'skills', text: 'runbook · built-in' },
  alert: { tone: 'warn', icon: 'siren' },
  enabled: { tone: 'ok', icon: 'checkCircle' },
  disabled: { tone: '', icon: 'pause' },
  // Miabi app health
  healthy: { tone: 'ok', icon: 'checkCircle' },
  unhealthy: { tone: 'danger', icon: 'xCircle' },
  degraded: { tone: 'warn', icon: 'alert' },
  miabi: { tone: 'accent', icon: 'layers' },
  // lessons
  proposed: { tone: 'warn', icon: 'lightbulb' },
  rejected: { tone: 'danger', icon: 'xCircle' },
}

const EFFECT: Record<string, Look> = {
  allow: { tone: 'ok', icon: 'check', text: 'allowed' },
  deny: { tone: 'danger', icon: 'ban', text: 'denied' },
  approve: { tone: 'warn', icon: 'approvals', text: 'needs approval' },
}

const RISK: Record<string, Look> = {
  low: { tone: 'ok', icon: 'approvals' },
  medium: { tone: 'info', icon: 'info' },
  high: { tone: 'warn', icon: 'alert' },
  critical: { tone: 'danger', icon: 'alert' },
  unknown: { tone: '', icon: 'info' },
}

const look = computed<Look>(() => {
  const v = props.value ?? ''
  if (props.kind === 'effect') return EFFECT[v] ?? { tone: '', icon: 'info' }
  if (props.kind === 'risk') return RISK[v] ?? RISK.unknown
  return STATUS[v] ?? { tone: '', icon: 'circle' }
})

const text = computed(() => {
  if (props.label) return props.label
  const v = props.value || 'unknown'
  if (props.kind === 'risk') return `${v} risk`
  return look.value.text ?? v.replace(/_/g, ' ')
})
</script>

<template>
  <span class="badge" :class="[look.tone, look.anim]">
    <Icon v-if="!noIcon" :name="look.icon" />{{ text }}
  </span>
</template>
