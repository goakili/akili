<!--
  Copyright (c) 2026 Jonas Kaninda.
  SPDX-License-Identifier: MIT
-->
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api, type AutonomySuggestion } from '../api'
import { useAuth } from '../stores/auth'
import { useToast } from '../stores/toast'
import { describeMatch, GRANT_DURATIONS } from '../lib/grants'
import { fmtDate, relTime } from '../lib/format'
import { useNow } from '../lib/now'
import GrantsPanel from '../views/autonomy/GrantsPanel.vue'
import EmptyState from './EmptyState.vue'
import Icon from './Icon'

const props = defineProps<{ agentId: string }>()

const auth = useAuth()
const toast = useToast()
const now = useNow()

const suggestions = ref<AutonomySuggestion[]>([])
const loading = ref(true)
const hours = ref<Record<string, number>>({})
const busy = ref('')
const grantsKey = ref(0)

async function load() {
  try {
    suggestions.value = (await api.agentAutonomy(props.agentId)).suggestions ?? []
  } catch {
    /* toasted */
  } finally {
    loading.value = false
  }
}

async function accept(s: AutonomySuggestion) {
  busy.value = s.key
  try {
    await api.acceptSuggestion(props.agentId, s.key, { expires_in_hours: hours.value[s.key] ?? 24 })
    toast.success(`Granted ${s.tool}`)
    suggestions.value = suggestions.value.filter((x) => x.key !== s.key)
    grantsKey.value++
  } catch {
    /* toasted */
  } finally {
    busy.value = ''
  }
}

async function dismiss(s: AutonomySuggestion) {
  busy.value = s.key
  try {
    await api.dismissSuggestion(props.agentId, s.key)
    suggestions.value = suggestions.value.filter((x) => x.key !== s.key)
  } catch {
    /* toasted */
  } finally {
    busy.value = ''
  }
}

onMounted(load)
</script>

<template>
  <div class="stack loose">
    <section class="card">
      <div class="card-head"><h2><Icon name="lightbulb" />Suggestions</h2></div>
      <div class="card-body stack">
        <p class="small muted" style="margin: 0">
          Drawn from the last 30 days of approvals for this agent. Nothing changes until someone accepts one; accepting creates a time-boxed grant as narrow as what was approved.
        </p>
        <div v-if="loading" class="small muted">Loading…</div>
        <EmptyState v-else-if="!suggestions.length" title="No suggestions" icon="lightbulb">
          A suggestion appears when the same call was approved often, by more than one person, and never declined.
        </EmptyState>
        <div v-for="s in suggestions" :key="s.key" class="rule-block stack">
          <div class="row wrap between">
            <div><span class="mono strong">{{ s.tool }}</span> <span class="muted small">{{ s.target }}</span></div>
            <span class="small muted" :title="fmtDate(s.last_at)">last {{ relTime(s.last_at, now) }}</span>
          </div>
          <div class="small">{{ s.text }}</div>
          <div v-if="s.match" class="small muted">Would cover: {{ describeMatch(s.match) }} · up to {{ s.max_risk }} risk</div>
          <div class="small muted">{{ s.approved }} approved · {{ s.denied }} declined · {{ s.expired }} expired</div>
          <div v-if="auth.isOperator" class="row wrap end" style="gap: 6px">
            <template v-if="s.kind === 'grant'">
              <select v-model.number="hours[s.key]" class="select sm" :aria-label="`Grant length for ${s.tool}`" style="width: 120px">
                <option v-for="[h, l] in GRANT_DURATIONS" :key="h" :value="h">{{ l }}</option>
              </select>
              <button type="button" class="btn btn-sm btn-primary" :disabled="busy === s.key" @click="accept(s)"><Icon name="key" />Grant</button>
            </template>
            <RouterLink v-else to="/policies" class="btn btn-sm"><Icon name="policies" />Open policies</RouterLink>
            <button type="button" class="btn btn-sm btn-ghost" :disabled="busy === s.key" @click="dismiss(s)">Dismiss</button>
          </div>
        </div>
      </div>
    </section>
    <GrantsPanel :key="grantsKey" :agent-id="agentId" />
  </div>
</template>
