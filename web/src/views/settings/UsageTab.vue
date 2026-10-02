<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api, type UsageRow } from '../../api'
import { useCatalog } from '../../stores/catalog'
import { compact, num, usd } from '../../lib/format'
import Icon from '../../components/Icon'
import EmptyState from '../../components/EmptyState.vue'
import SkeletonRows from '../../components/SkeletonRows.vue'

const catalog = useCatalog()
const rows = ref<UsageRow[]>([])
const days = ref(30)
const loading = ref(true)

async function load() {
  loading.value = true
  try {
    rows.value = (await api.usage(days.value)) ?? []
  } catch {
    /* toasted */
  } finally {
    loading.value = false
  }
}

const totals = computed(() =>
  rows.value.reduce(
    (t, r) => ({ calls: t.calls + r.calls, input: t.input + r.input_tokens, output: t.output + r.output_tokens, cost: t.cost + r.cost_usd }),
    { calls: 0, input: 0, output: 0, cost: 0 },
  ),
)
const maxCost = computed(() => Math.max(0.000001, ...rows.value.map((r) => r.cost_usd)))

onMounted(() => {
  load()
  catalog.loadAgents()
})
</script>

<template>
  <div class="stack">
    <div class="row between wrap">
      <div class="row">
        <label for="us-days" class="label">Period</label>
        <select id="us-days" v-model.number="days" class="select sm" style="width: 160px" @change="load">
          <option :value="1">Last 24 hours</option>
          <option :value="7">Last 7 days</option>
          <option :value="30">Last 30 days</option>
          <option :value="90">Last 90 days</option>
          <option :value="365">Last year</option>
        </select>
      </div>
    </div>
    <div class="stats">
      <div class="stat"><span class="stat-icon tone-info" aria-hidden="true"><Icon name="dollar" /></span><div><div class="stat-label">Spend</div><div class="stat-value">{{ usd(totals.cost) }}</div></div></div>
      <div class="stat"><span class="stat-icon" aria-hidden="true"><Icon name="zap" /></span><div><div class="stat-label">Model calls</div><div class="stat-value">{{ num(totals.calls) }}</div></div></div>
      <div class="stat"><span class="stat-icon tone-violet" aria-hidden="true"><Icon name="arrowDown" /></span><div><div class="stat-label">Input tokens</div><div class="stat-value">{{ compact(totals.input) }}</div></div></div>
      <div class="stat"><span class="stat-icon tone-success" aria-hidden="true"><Icon name="send" /></span><div><div class="stat-label">Output tokens</div><div class="stat-value">{{ compact(totals.output) }}</div></div></div>
    </div>
    <div class="card">
      <div class="table-wrap">
        <table class="table">
          <thead><tr><th>Agent</th><th>Model</th><th class="right">Calls</th><th class="right">Input</th><th class="right">Output</th><th class="right">Cost</th><th style="width: 22%"><span class="sr-only">Share</span></th></tr></thead>
          <tbody>
            <SkeletonRows v-if="loading" :cols="7" :rows="3" />
            <tr v-else-if="!rows.length"><td colspan="7"><EmptyState title="No model usage in this period" icon="gauge" compact>Spend and tokens appear here once agents call a model.</EmptyState></td></tr>
            <tr v-for="r in rows" :key="r.agent_id + r.model">
              <td><RouterLink v-if="r.agent_id" :to="`/agents/${r.agent_id}`">{{ catalog.agentName(r.agent_id) }}</RouterLink><span v-else class="muted">—</span></td>
              <td class="mono small">{{ r.model }}</td>
              <td class="right num">{{ num(r.calls) }}</td>
              <td class="right">{{ num(r.input_tokens) }}</td>
              <td class="right">{{ num(r.output_tokens) }}</td>
              <td class="right">{{ usd(r.cost_usd) }}</td>
              <td>
                <div style="height: 6px; border-radius: 3px; background: var(--bg-tertiary)" aria-hidden="true">
                  <div :style="{ width: `${(r.cost_usd / maxCost) * 100}%`, height: '100%', borderRadius: '3px', background: 'var(--primary-500)' }" />
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>
