<!--
  Copyright (c) 2026 Jonas Kaninda.
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
import { computed } from 'vue'
import Icon, { type IconName } from './Icon'

// Never colour alone: every tone carries an icon and a word.
const props = defineProps<{ value: string | null | undefined; kind?: 'risk' | 'effect' | 'status'; label?: string; noIcon?: boolean }>()

type Tone = '' | 'ok' | 'warn' | 'danger' | 'info' | 'accent' | 'violet'
interface Look {
  tone: Tone
  icon: IconName
  anim?: 'spin' | 'pulse'
  text?: string
}

const STATUS: Record<string, Look> = {
  online: { tone: 'ok', icon: 'circleDot' },
  offline: { tone: '', icon: 'circle' },
  pending: { tone: 'warn', icon: 'hourglass' },
  revoked: { tone: 'danger', icon: 'ban' },
  draining: { tone: 'warn', icon: 'pause' },
  draft: { tone: '', icon: 'edit' },
  queued: { tone: '', icon: 'clock' },
  assigned: { tone: 'info', icon: 'loader', anim: 'spin' },
  running: { tone: 'info', icon: 'loader', anim: 'spin' },
  succeeded: { tone: 'ok', icon: 'checkCircle' },
  failed: { tone: 'danger', icon: 'xCircle' },
  cancelled: { tone: '', icon: 'ban' },
  timed_out: { tone: 'danger', icon: 'hourglass', text: 'timed out' },
  needs_approval: { tone: 'warn', icon: 'approvals', text: 'needs approval' },
  approved: { tone: 'ok', icon: 'checkCircle' },
  denied: { tone: 'danger', icon: 'xCircle' },
  expired: { tone: '', icon: 'hourglass' },
  open: { tone: 'ok', icon: 'circleDot' },
  closed: { tone: '', icon: 'lock' },
  idle: { tone: '', icon: 'circle' },
  thinking: { tone: 'info', icon: 'brain', anim: 'pulse' },
  running_tool: { tone: 'violet', icon: 'wrench', anim: 'pulse', text: 'running tool' },
  waiting_approval: { tone: 'warn', icon: 'approvals', anim: 'pulse', text: 'waiting for approval' },
  waiting_input: { tone: 'info', icon: 'help', anim: 'pulse', text: 'waiting for your answer' },
  needs_input: { tone: 'info', icon: 'help', text: 'needs your answer' },
  chat: { tone: 'info', icon: 'chat' },
  task: { tone: 'violet', icon: 'tasks' },
  ok: { tone: 'ok', icon: 'check' },
  error: { tone: 'danger', icon: 'x' },
  active: { tone: 'ok', icon: 'checkCircle' },
  'used up': { tone: '', icon: 'ban' },
  freeze: { tone: 'danger', icon: 'lock' },
  window: { tone: 'info', icon: 'clock' },
  inactive: { tone: '', icon: 'circle' },
  default: { tone: 'accent', icon: 'zap' },
  builtin: { tone: 'info', icon: 'lock', text: 'built-in' },
  rolled_back: { tone: 'warn', icon: 'undo', text: 'rolled back' },
  skipped: { tone: '', icon: 'skip' },
  in_progress: { tone: 'info', icon: 'loader', text: 'in progress' },
  done: { tone: 'ok', icon: 'checkCircle' },
  archived: { tone: '', icon: 'box' },
  todo: { tone: '', icon: 'circle', text: 'to do' },
  check_failed: { tone: 'danger', icon: 'xCircle', text: 'check failed' },
  runbook: { tone: 'violet', icon: 'skills', text: 'runbook · built-in' },
  alert: { tone: 'warn', icon: 'siren' },
  enabled: { tone: 'ok', icon: 'checkCircle' },
  disabled: { tone: '', icon: 'pause' },
  healthy: { tone: 'ok', icon: 'checkCircle' },
  unhealthy: { tone: 'danger', icon: 'xCircle' },
  degraded: { tone: 'warn', icon: 'alert' },
  miabi: { tone: 'accent', icon: 'layers' },
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
