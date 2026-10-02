<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
// A change plan (the input of a change_run call), rendered for a human to decide on: what will
// change, how success is checked, and how it is undone. Never raw JSON.
import { computed, onMounted } from 'vue'
import type { ChangePlan, PlanCall } from '../api'
import { useCatalog } from '../stores/catalog'
import { callArgs, callSummary, PHASES, toolIcon } from '../lib/tools'
import Badge from './Badge.vue'
import Icon from './Icon'

const props = withDefaults(defineProps<{ plan: ChangePlan; risk?: string; compact?: boolean; hideTitle?: boolean }>(), {
  risk: '',
  compact: false,
  hideTitle: false,
})
const catalog = useCatalog()
onMounted(() => catalog.loadTools())

const groups = computed(() =>
  PHASES.map((p) => ({ ...p, calls: (p.key === 'step' ? props.plan.steps : p.key === 'verify' ? props.plan.verify : props.plan.rollback) ?? [] })),
)
const noRollback = computed(() => !(props.plan.rollback ?? []).length)
const counts = computed(() => {
  const n = (k: number, one: string, many: string) => `${k} ${k === 1 ? one : many}`
  return [n(props.plan.steps.length, 'step', 'steps'), n(props.plan.verify.length, 'check', 'checks'), noRollback.value ? 'no rollback' : n((props.plan.rollback ?? []).length, 'rollback call', 'rollback calls')]
})

function callRisk(c: PlanCall): string {
  return catalog.toolRisk(c.tool)
}
</script>

<template>
  <div class="plan" :class="{ compact }">
    <div v-if="!hideTitle" class="plan-head">
      <Icon name="clipboard" class="plan-ic" />
      <strong class="plan-title">{{ plan.title || 'Untitled change' }}</strong>
      <Badge v-if="risk" :value="risk" kind="risk" :label="`highest risk: ${risk}`" />
    </div>
    <p v-if="plan.reason" class="plan-reason">{{ plan.reason }}</p>
    <div class="plan-counts xs">
      <span v-for="c in counts" :key="c" :class="{ 'text-warn strong': c === 'no rollback' }">{{ c }}</span>
    </div>

    <div v-if="noRollback" class="banner warn plan-warn" role="note">
      <Icon name="alert" />
      <div class="banner-body"><strong>No rollback.</strong> These changes cannot be undone automatically: if a step or check fails, a human has to repair it.</div>
    </div>

    <template v-if="compact">
      <ol class="plan-mini">
        <li v-for="(c, i) in plan.steps" :key="i">
          <span class="pc-num" aria-hidden="true">{{ i + 1 }}</span>
          <Icon :name="toolIcon(c.tool)" />
          <span class="mono strong">{{ c.tool }}</span>
          <span class="mono muted truncate">{{ callSummary(c.input, c.tool) }}</span>
        </li>
      </ol>
    </template>
    <template v-else>
      <section v-for="g in groups" :key="g.key" class="plan-phase" :class="`ph-${g.key}`">
        <template v-if="g.calls.length">
          <h4 class="plan-phase-title">
            <Icon :name="g.icon" />{{ g.title }}<span class="count">{{ g.calls.length }}</span>
            <span class="xs muted plan-phase-help hide-mobile">{{ g.help }}</span>
          </h4>
          <ol class="plan-calls">
            <li v-for="(c, i) in g.calls" :key="i" class="plan-call">
              <span class="pc-num" aria-hidden="true">{{ i + 1 }}</span>
              <div class="pc-main">
                <div class="pc-line">
                  <span class="pc-ic" aria-hidden="true"><Icon :name="toolIcon(c.tool)" /></span>
                  <span class="mono strong">{{ c.tool }}</span>
                  <Badge v-if="callRisk(c)" :value="callRisk(c)" kind="risk" />
                </div>
                <div v-if="c.description" class="pc-desc">{{ c.description }}</div>
                <div v-if="callArgs(c.input).length" class="pc-args">
                  <code v-for="a in callArgs(c.input)" :key="a.key" class="pc-arg"><span class="muted">{{ a.key }}=</span>{{ a.value }}</code>
                </div>
                <div v-if="c.expect || c.reject" class="pc-checks xs">
                  <span v-if="c.expect" class="pc-expect"><Icon name="check" :size="12" />output must contain <code>{{ c.expect }}</code></span>
                  <span v-if="c.reject" class="pc-reject"><Icon name="ban" :size="12" />must not contain <code>{{ c.reject }}</code></span>
                </div>
              </div>
            </li>
          </ol>
        </template>
      </section>
    </template>
  </div>
</template>

<style scoped>
.plan {
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-width: 0;
}
.plan-head {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.plan-ic {
  color: var(--primary-text);
  width: 16px;
  height: 16px;
}
.plan-phase-title .icon {
  width: 14px;
  height: 14px;
}
.plan-title {
  font-size: 14.5px;
}
.plan-reason {
  margin: 0;
  color: var(--text-secondary);
  font-size: 13.5px;
  white-space: pre-wrap;
}
.compact .plan-reason {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.plan-counts {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 12px;
  color: var(--text-tertiary);
}
.plan-counts span + span::before {
  content: '·';
  margin-right: 12px;
  color: var(--text-muted);
}
.text-warn {
  color: var(--warning-text);
}
.plan-warn {
  margin: 0;
}
.plan-phase-title {
  display: flex;
  align-items: center;
  gap: 6px;
  margin: 4px 0 6px;
  font-size: 12px;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--text-secondary);
}
.plan-phase-title .count {
  font-size: 11px;
  padding: 0 6px;
  border-radius: 999px;
  background: var(--bg-tertiary);
  color: var(--text-tertiary);
}
.plan-phase-help {
  text-transform: none;
  letter-spacing: 0;
  font-weight: 400;
  margin-left: auto;
}
.plan-phase.ph-rollback .plan-phase-title {
  color: var(--warning-text);
}
.plan-calls,
.plan-mini {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.plan-call {
  display: flex;
  gap: 10px;
  padding: 8px 10px;
  border: 1px solid var(--border-primary);
  border-radius: var(--radius);
  background: var(--bg-primary);
}
.pc-num {
  flex: none;
  width: 20px;
  height: 20px;
  border-radius: 50%;
  display: inline-grid;
  place-items: center;
  font-size: 11px;
  font-weight: 600;
  background: var(--bg-tertiary);
  color: var(--text-secondary);
}
.pc-main {
  min-width: 0;
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.pc-line {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.pc-ic {
  width: 22px;
  height: 22px;
  border-radius: var(--radius-sm);
  display: inline-grid;
  place-items: center;
  background: var(--bg-tertiary);
  color: var(--text-secondary);
}
.pc-ic .icon {
  width: 13px;
  height: 13px;
}
.pc-desc {
  font-size: 13px;
  color: var(--text-primary);
}
.pc-args {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
.pc-arg {
  font-family: var(--mono);
  font-size: 11.5px;
  padding: 1px 6px;
  border-radius: var(--radius-sm);
  background: var(--bg-tertiary);
  color: var(--text-primary);
  max-width: 100%;
  overflow-wrap: anywhere;
}
.pc-checks {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 12px;
}
.pc-checks > span {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}
.pc-expect {
  color: var(--success-text);
}
.pc-reject {
  color: var(--danger-text);
}
.pc-checks code {
  font-family: var(--mono);
  color: var(--text-primary);
}
.plan-mini li {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  font-size: 12.5px;
}
.plan-mini .icon {
  width: 13px;
  height: 13px;
  flex: none;
  color: var(--text-tertiary);
}
</style>
