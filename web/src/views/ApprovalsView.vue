<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { api, type Approval, type ApprovalStatus, type Change } from '../api'
import { useAuth } from '../stores/auth'
import { useCatalog } from '../stores/catalog'
import { useLive } from '../stores/live'
import ApprovalCard from '../components/ApprovalCard.vue'
import PageHeader from '../components/PageHeader.vue'
import EmptyState from '../components/EmptyState.vue'
import InfiniteScroll from '../components/InfiniteScroll.vue'
import { usePaged } from '../lib/paged'

const auth = useAuth()
const catalog = useCatalog()
const live = useLive()
const status = ref<ApprovalStatus | ''>('pending')
const paged = usePaged((page) => api.pageApprovals({ status: status.value || undefined, page }))
const { items, loading, loadingMore, hasMore } = paged
const list = ref<HTMLElement | null>(null)
/** approval id → change id, for change_run approvals whose link arrived live after the approval. */
const changeOf = ref<Map<string, string>>(new Map())

const FILTERS: { value: ApprovalStatus | ''; label: string }[] = [
  { value: 'pending', label: 'Pending' },
  { value: 'approved', label: 'Approved' },
  { value: 'denied', label: 'Denied' },
  { value: 'expired', label: 'Expired' },
  { value: '', label: 'All' },
]

function setStatus(s: ApprovalStatus | '') {
  status.value = s
  items.value = []
  paged.reload()
}

function upsert(a: Approval) {
  const i = items.value.findIndex((x) => x.id === a.id)
  const matches = !status.value || a.status === status.value
  if (i >= 0) {
    // Keep keyboard flow: after deciding the focused card, move focus to the next one.
    const hadFocus = list.value?.children[i]?.contains(document.activeElement)
    if (matches) items.value[i] = a
    else {
      items.value.splice(i, 1)
      if (hadFocus) {
        requestAnimationFrame(() => {
          const next = list.value?.children[Math.min(i, items.value.length - 1)] as HTMLElement | undefined
          next?.querySelector<HTMLElement>('.approval-card')?.focus()
        })
      }
    }
  } else if (matches) items.value.unshift(a)
  live.refreshCounts()
}

let off: (() => void) | null = null
let offRe: (() => void) | null = null
onMounted(() => {
  paged.reload()
  catalog.loadAgents()
  off = live.on((ev) => {
    if ((ev.type === 'approval.created' || ev.type === 'approval.resolved') && ev.data) upsert(ev.data as Approval)
    if (ev.type === 'change.updated' && ev.data) {
      const c = ev.data as Change
      if (c.approval_id && !changeOf.value.has(c.approval_id)) changeOf.value = new Map(changeOf.value).set(c.approval_id, c.id)
    }
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
    <PageHeader title="Approvals" subtitle="Risky tool calls and change plans wait here for a human. Each decision applies to that exact call or plan only." />

    <div class="toolbar">
      <div class="segmented" role="group" aria-label="Filter by status">
        <button v-for="f in FILTERS" :key="f.value" type="button" :class="{ on: status === f.value }" :aria-pressed="status === f.value" @click="setStatus(f.value)">
          {{ f.label }}<span v-if="f.value === 'pending' && live.pendingApprovals" class="count">{{ live.pendingApprovals }}</span>
        </button>
      </div>
      <span v-if="auth.isOperator && status === 'pending' && items.length" class="small muted hide-mobile" style="margin-left: auto">
        Focus a card, then press <kbd>A</kbd> to approve or <kbd>D</kbd> to deny.
      </span>
    </div>

    <div v-if="loading && !items.length" class="stack" aria-busy="true">
      <div v-for="i in 3" :key="i" class="skel skel-card" style="height: 150px" />
    </div>
    <div v-else-if="!items.length" class="card">
      <EmptyState :title="status === 'pending' ? 'Inbox zero' : 'No approvals'" :icon="status === 'pending' ? 'checkCircle' : 'approvals'">
        {{ status === 'pending' ? 'No tool calls are waiting for a decision. New requests appear here live.' : 'Nothing matches this filter.' }}
      </EmptyState>
    </div>
    <div v-else ref="list" class="stack">
      <div v-for="a in items" :key="a.id">
        <ApprovalCard :approval="a" show-context flat :keyboard="auth.isOperator" :change-id="a.change_id ?? changeOf.get(a.id)" @resolved="upsert" />
      </div>
    </div>
    <InfiniteScroll v-if="items.length" :has-more="hasMore" :loading="loadingMore" @more="paged.more" />
  </div>
</template>
