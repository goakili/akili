<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, type Change, type ChangeStatus } from '../api'
import { useCatalog } from '../stores/catalog'
import { useLive } from '../stores/live'
import { fmtDate, relTime } from '../lib/format'
import { useNow } from '../lib/now'
import { usePaged } from '../lib/paged'
import Badge from '../components/Badge.vue'
import PageHeader from '../components/PageHeader.vue'
import EmptyState from '../components/EmptyState.vue'
import InfiniteScroll from '../components/InfiniteScroll.vue'
import SkeletonRows from '../components/SkeletonRows.vue'
import Icon from '../components/Icon'

const catalog = useCatalog()
const live = useLive()
const route = useRoute()
const router = useRouter()
const now = useNow()

const FILTERS: { value: ChangeStatus | ''; label: string }[] = [
  { value: '', label: 'All' },
  { value: 'pending', label: 'Pending' },
  { value: 'running', label: 'Running' },
  { value: 'succeeded', label: 'Succeeded' },
  { value: 'rolled_back', label: 'Rolled back' },
  { value: 'failed', label: 'Failed' },
  { value: 'denied', label: 'Denied' },
  { value: 'expired', label: 'Expired' },
]

const status = computed<ChangeStatus | ''>(() => (FILTERS.find((f) => f.value && f.value === route.query.status)?.value ?? '') as ChangeStatus | '')
const agentFilter = computed(() => (typeof route.query.agent === 'string' ? route.query.agent : ''))
const paged = usePaged((page) => api.pageChanges({ status: status.value || undefined, agent_id: agentFilter.value || undefined, page }))
const { items, loading, loadingMore, hasMore } = paged
const load = paged.reload

function setStatus(s: ChangeStatus | '') {
  items.value = []
  router.replace({ query: { ...route.query, status: s || undefined } }).then(load)
}

function clearAgent() {
  router.replace({ query: { ...route.query, agent: undefined } }).then(load)
}

function upsert(c: Change) {
  const i = items.value.findIndex((x) => x.id === c.id)
  const matches = (!status.value || c.status === status.value) && (!agentFilter.value || c.agent_id === agentFilter.value)
  if (i >= 0) {
    if (matches) items.value[i] = c
    else items.value.splice(i, 1)
  } else if (matches) items.value.unshift(c)
}

function counts(c: Change) {
  const calls = c.calls ?? []
  const n = (p: string) => calls.filter((x) => x.phase === p).length
  return { steps: n('step'), verify: n('verify'), rollback: n('rollback') }
}

const plural = (n: number, w: string) => `${n} ${w}${n === 1 ? '' : 's'}`
const open = (id: string) => router.push(`/changes/${id}`)

let off: (() => void) | null = null
let offRe: (() => void) | null = null
onMounted(() => {
  load()
  catalog.loadAgents()
  off = live.on((ev) => {
    if (ev.type === 'change.updated' && ev.data) upsert(ev.data as Change)
  })
  offRe = live.onReconnect(paged.refresh)
})
onUnmounted(() => {
  off?.()
  offRe?.()
})
</script>

<template>
  <div>
    <PageHeader title="Changes" subtitle="Change plans agents propose to fix what they find. A human approves each plan as a whole; the agent runs it, verifies it and rolls back on failure." />

    <div class="toolbar">
      <div class="segmented" role="group" aria-label="Filter by status">
        <button v-for="f in FILTERS" :key="f.value" type="button" :class="{ on: status === f.value }" :aria-pressed="status === f.value" @click="setStatus(f.value)">
          {{ f.label }}
        </button>
      </div>
      <span v-if="agentFilter" class="badge outline" style="margin-left: auto">
        <Icon name="agents" />{{ catalog.agentName(agentFilter) }}
        <button type="button" class="btn btn-ghost btn-xs btn-icon" aria-label="Clear agent filter" @click="clearAgent"><Icon name="x" /></button>
      </span>
    </div>

    <div class="card">
      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr><th>Change</th><th class="hide-mobile">Agent</th><th class="hide-mobile">Risk</th><th class="hide-mobile">Created</th><th>Status</th></tr>
          </thead>
          <tbody>
            <SkeletonRows v-if="loading && !items.length" :cols="5" :rows="4" />
            <tr v-else-if="!items.length">
              <td colspan="5">
                <EmptyState :title="status ? 'No matching changes' : 'No change plans yet'" icon="clipboard">
                  <template v-if="status">Nothing matches this filter.</template>
                  <template v-else>When an agent wants to change a host (restart a service, free disk space…) it proposes a plan with steps, checks and a rollback. Plans appear here and wait for approval.</template>
                </EmptyState>
              </td>
            </tr>
            <tr v-for="c in items" :key="c.id" class="clickable" tabindex="0" @click="open(c.id)" @keydown.enter="open(c.id)">
              <td style="max-width: 460px">
                <RouterLink :to="`/changes/${c.id}`" class="cell-title truncate" style="display: block" @click.stop>{{ c.title }}</RouterLink>
                <div class="cell-sub truncate">{{ c.reason }}</div>
                <div class="xs muted" style="margin-top: 2px">
                  {{ plural(counts(c).steps, 'step') }} · {{ plural(counts(c).verify, 'check') }} ·
                  <span :class="{ 'warn-text': !counts(c).rollback }">{{ counts(c).rollback ? `${counts(c).rollback} rollback` : 'no rollback' }}</span>
                </div>
              </td>
              <td class="nowrap hide-mobile">
                <RouterLink :to="`/agents/${c.agent_id}`" @click.stop>{{ catalog.agentName(c.agent_id) }}</RouterLink>
              </td>
              <td class="hide-mobile"><Badge v-if="c.risk" :value="c.risk" kind="risk" /></td>
              <td class="nowrap hide-mobile" :title="fmtDate(c.created_at)">{{ relTime(c.created_at, now) }}</td>
              <td><Badge :value="c.status" /></td>
            </tr>
          </tbody>
        </table>
      </div>
      <InfiniteScroll :has-more="hasMore" :loading="loadingMore" @more="paged.more" />
    </div>
  </div>
</template>

<style scoped>
.warn-text {
  color: var(--warning-text);
  font-weight: 600;
}
</style>
