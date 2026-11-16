<!--
  Copyright (c) 2026 Jonas Kaninda.
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { api, type Review } from '../api'
import { useAuth } from '../stores/auth'
import { useCatalog } from '../stores/catalog'
import { useToast } from '../stores/toast'
import { relTime } from '../lib/format'
import { useNow } from '../lib/now'
import Badge from './Badge.vue'
import Icon from './Icon'

const props = withDefaults(defineProps<{ targetType: 'change' | 'task'; targetId: string; authorAgentId?: string | null; compact?: boolean }>(), {
  authorAgentId: null,
  compact: false,
})

const auth = useAuth()
const catalog = useCatalog()
const toast = useToast()
const now = useNow()

const reviews = ref<Review[]>([])
const reviewer = ref('')
const busy = ref(false)
let timer: number | undefined

const candidates = computed(() => catalog.agents.filter((a) => a.id !== props.authorAgentId && a.status !== 'revoked'))
const pending = computed(() => reviews.value.some((r) => r.status === 'pending'))
const VERDICT: Record<string, string> = { looks_good: 'looks good', concerns: 'concerns', block: 'would block' }

async function load() {
  try {
    reviews.value = (await api.listReviews({ target_type: props.targetType, target_id: props.targetId }, { quiet: true })) ?? []
  } catch {
    /* the panel stays empty */
  }
  // A reviewer works for a while: poll until every review has an answer.
  clearTimeout(timer)
  if (pending.value) timer = window.setTimeout(load, 5000)
}

async function request() {
  if (!reviewer.value) return
  busy.value = true
  try {
    await api.requestReview(props.targetType, props.targetId, { agent_id: reviewer.value })
    toast.success(`Review requested from ${catalog.agentName(reviewer.value)}`)
    reviewer.value = ''
    load()
  } catch {
    /* toasted */
  } finally {
    busy.value = false
  }
}

onMounted(() => {
  load()
  catalog.loadAgents()
})
onUnmounted(() => clearTimeout(timer))
</script>

<template>
  <div class="stack tight review-panel">
    <div v-for="r in reviews" :key="r.id" class="rule-block stack tight">
      <div class="row wrap between">
        <div class="row wrap" style="gap: 6px">
          <Icon name="eye" />
          <strong>Review by {{ r.reviewer_agent_id ? catalog.agentName(r.reviewer_agent_id) : 'an agent' }}</strong>
          <span class="small muted">advisory</span>
          <Badge v-if="r.status !== 'done'" :value="r.status" />
          <Badge v-else-if="r.verdict" :value="r.verdict === 'looks_good' ? 'ok' : r.verdict === 'block' ? 'error' : 'pending'" :label="VERDICT[r.verdict]" no-icon />
        </div>
        <span class="small muted">{{ relTime(r.updated_at, now) }}</span>
      </div>
      <template v-if="r.status !== 'pending'">
        <div v-if="r.summary" class="small" style="white-space: pre-wrap">{{ r.summary }}</div>
        <template v-if="!compact">
          <ul v-if="r.risks?.length" class="small" style="margin: 0"><li v-for="(x, i) in r.risks" :key="`r${i}`"><strong>Risk:</strong> {{ x }}</li></ul>
          <ul v-if="r.concerns?.length" class="small" style="margin: 0"><li v-for="(x, i) in r.concerns" :key="`c${i}`"><strong>Concern:</strong> {{ x }}</li></ul>
        </template>
        <RouterLink v-if="r.reviewer_task_id" :to="`/tasks/${r.reviewer_task_id}`" class="small">Reviewer's task</RouterLink>
      </template>
      <span v-else class="small muted">The reviewer is reading, with read-only tools.</span>
    </div>
    <form v-if="auth.isOperator" class="row wrap" style="gap: 6px" @submit.prevent="request">
      <label :for="`rv-${targetId}`" class="sr-only">Reviewer agent</label>
      <select :id="`rv-${targetId}`" v-model="reviewer" class="select sm" style="max-width: 220px">
        <option value="" disabled>Ask an agent to review…</option>
        <option v-for="a in candidates" :key="a.id" :value="a.id">{{ a.name }}</option>
      </select>
      <button type="submit" class="btn btn-sm" :disabled="busy || !reviewer"><Icon name="eye" />Request review</button>
      <span class="small muted">Read-only; its verdict is advice, never a decision.</span>
    </form>
  </div>
</template>
