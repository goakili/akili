<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, type AgentStatus, type Enrollment } from '../api'
import { useAuth } from '../stores/auth'
import { useCatalog } from '../stores/catalog'
import { useLive } from '../stores/live'
import { useToast } from '../stores/toast'
import { agentFormFrom, agentInput } from '../lib/agentForm'
import { relTime } from '../lib/format'
import { useNow } from '../lib/now'
import Badge from '../components/Badge.vue'
import Modal from '../components/Modal.vue'
import AgentFields from '../components/AgentFields.vue'
import EnrollmentPanel from '../components/EnrollmentPanel.vue'
import PageHeader from '../components/PageHeader.vue'
import EmptyState from '../components/EmptyState.vue'
import SkeletonRows from '../components/SkeletonRows.vue'
import Icon from '../components/Icon'

const auth = useAuth()
const catalog = useCatalog()
const live = useLive()
const toast = useToast()
const route = useRoute()
const router = useRouter()
const now = useNow()

const loading = ref(true)
const filter = ref('')
const status = ref<AgentStatus | ''>('')
const showCreate = ref(false)
const form = ref(agentFormFrom())
const busy = ref(false)
const tried = ref(false)
const enrollment = ref<Enrollment | null>(null)

async function load() {
  await Promise.all([catalog.loadAgents(true), catalog.loadPolicies().catch(() => {})])
  loading.value = false
}

const STATUSES: { value: AgentStatus | ''; label: string }[] = [
  { value: '', label: 'All' },
  { value: 'online', label: 'Online' },
  { value: 'offline', label: 'Offline' },
  { value: 'pending', label: 'Pending' },
  { value: 'revoked', label: 'Revoked' },
]
const counts = computed(() => {
  const c: Record<string, number> = { '': catalog.agents.length }
  for (const a of catalog.agents) c[a.status] = (c[a.status] ?? 0) + 1
  return c
})

const agents = computed(() => {
  const q = filter.value.trim().toLowerCase()
  let list = [...catalog.agents].sort((a, b) => a.name.localeCompare(b.name))
  if (status.value) list = list.filter((a) => a.status === status.value)
  if (!q) return list
  return list.filter(
    (a) =>
      a.name.toLowerCase().includes(q) ||
      (a.labels ?? []).some((l) => l.toLowerCase().includes(q)) ||
      (a.facts?.hostname ?? '').toLowerCase().includes(q),
  )
})

// Mirrors the server's default for agents created without a policy (fleet.DefaultPolicy).
const DEFAULT_POLICY = 'read-only'

async function openCreate() {
  form.value = agentFormFrom()
  if (!catalog.policies.length) await catalog.loadPolicies().catch(() => {})
  form.value.policy_id = catalog.policies.find((p) => p.builtin && p.name === DEFAULT_POLICY)?.id ?? ''
  enrollment.value = null
  tried.value = false
  showCreate.value = true
}

function closeCreate() {
  showCreate.value = false
  if (route.query.new) router.replace({ query: {} })
}

async function create() {
  tried.value = true
  if (!form.value.name.trim()) return
  busy.value = true
  try {
    const enr = await api.createAgent(agentInput(form.value))
    enrollment.value = enr
    catalog.upsertAgent(enr.agent)
    toast.success(`Agent ${enr.agent.name} created`)
  } catch {
    /* toasted */
  } finally {
    busy.value = false
  }
}

let off: (() => void) | null = null
let offRe: (() => void) | null = null
let t: ReturnType<typeof setTimeout> | null = null
onMounted(() => {
  load()
  if (route.query.new && auth.isAdmin) openCreate()
  off = live.on((ev) => {
    if (ev.type === 'agent.status' && ev.agent_id) {
      catalog.patchAgentStatus(ev.agent_id, (ev.data as { status?: string } | undefined)?.status ?? 'offline')
      if (t) clearTimeout(t)
      t = setTimeout(() => catalog.loadAgents(true), 800)
    }
  })
  offRe = live.onReconnect(() => catalog.loadAgents(true))
})
onUnmounted(() => {
  off?.()
  offRe?.()
  if (t) clearTimeout(t)
})

const open = (id: string) => router.push(`/agents/${id}`)
</script>

<template>
  <div>
    <PageHeader title="Agents" subtitle="Enrolled machines, what they may do and how they are doing.">
      <button v-if="auth.isAdmin" type="button" class="btn btn-primary" @click="openCreate"><Icon name="plus" />Add agent</button>
    </PageHeader>

    <div class="toolbar">
      <div class="segmented" role="group" aria-label="Filter by status">
        <button v-for="s in STATUSES" :key="s.value" type="button" :class="{ on: status === s.value }" :aria-pressed="status === s.value" @click="status = s.value">
          {{ s.label }}<span class="count">{{ counts[s.value] ?? 0 }}</span>
        </button>
      </div>
      <div class="search-input" style="margin-left: auto; width: min(300px, 100%)">
        <Icon name="search" />
        <input v-model="filter" class="input" type="search" placeholder="Search name, label or host" aria-label="Search agents" />
      </div>
    </div>

    <div class="card">
      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr>
              <th>Agent</th>
              <th>Status</th>
              <th class="hide-mobile">Labels</th>
              <th>Policy</th>
              <th class="hide-mobile">Autonomy</th>
              <th class="hide-mobile">Host</th>
              <th class="hide-mobile">Last seen</th>
              <th class="right hide-mobile">Sessions</th>
            </tr>
          </thead>
          <tbody>
            <SkeletonRows v-if="loading && !catalog.agents.length" :cols="8" :rows="4" />
            <tr v-else-if="!agents.length">
              <td colspan="8">
                <EmptyState v-if="filter || status" title="No matching agents" icon="search">
                  Try a different search or status filter.
                  <template #actions><button type="button" class="btn btn-sm" @click="(filter = ''), (status = '')">Clear filters</button></template>
                </EmptyState>
                <EmptyState v-else title="No agents yet" icon="agents">
                  An agent is a small daemon on a host you control. Adding one gives you a one-time join token and an install command.
                  <template v-if="auth.isAdmin" #actions><button type="button" class="btn btn-primary" @click="openCreate"><Icon name="plus" />Add your first agent</button></template>
                </EmptyState>
              </td>
            </tr>
            <tr
              v-for="a in agents"
              :key="a.id"
              class="clickable"
              tabindex="0"
              :aria-label="`Open agent ${a.name}`"
              @click="open(a.id)"
              @keydown.enter="open(a.id)"
            >
              <td>
                <div class="row" style="gap: 10px">
                  <span class="status-dot" :class="a.status" aria-hidden="true" />
                  <div style="min-width: 0">
                    <RouterLink :to="`/agents/${a.id}`" class="cell-title" @click.stop>{{ a.name }}</RouterLink>
                    <div v-if="a.description" class="cell-sub truncate" style="max-width: 260px">{{ a.description }}</div>
                  </div>
                </div>
              </td>
              <td>
                <div class="row" style="gap: 4px">
                  <Badge :value="a.status" />
                  <Badge v-if="a.draining" value="draining" />
                </div>
              </td>
              <td class="hide-mobile">
                <span v-for="l in a.labels ?? []" :key="l" class="label-chip">{{ l }}</span>
                <span v-if="!(a.labels ?? []).length" class="faint">—</span>
              </td>
              <td class="nowrap">
                <span v-if="a.policy_id">{{ catalog.policyName(a.policy_id) }}</span>
                <span v-else class="badge warn" title="No policy: every tool call is denied"><Icon name="alert" />none</span>
              </td>
              <td class="hide-mobile"><span class="badge outline square">L{{ a.autonomy }}</span></td>
              <td class="hide-mobile">
                <div class="nowrap">{{ a.facts?.hostname || '—' }}</div>
                <div class="cell-sub nowrap">{{ [a.facts?.os, a.facts?.arch].filter(Boolean).join('/') }}</div>
              </td>
              <td class="nowrap hide-mobile" :title="a.last_seen_at ?? ''">{{ relTime(a.last_seen_at, now) }}</td>
              <td class="right num hide-mobile">{{ a.active_sessions }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <Modal :open="showCreate" :title="enrollment ? 'Enroll your agent' : 'Add agent'" wide :dismissable="!busy" @close="closeCreate">
      <EnrollmentPanel v-if="enrollment" :enrollment="enrollment" />
      <form v-else id="create-agent" novalidate @submit.prevent="create">
        <AgentFields v-model="form" :disabled="busy" :show-errors="tried" />
      </form>
      <template #footer>
        <template v-if="enrollment">
          <RouterLink :to="`/agents/${enrollment.agent.id}`" class="btn">Open agent</RouterLink>
          <button type="button" class="btn btn-primary" @click="closeCreate">Done</button>
        </template>
        <template v-else>
          <button type="button" class="btn" @click="closeCreate">Cancel</button>
          <button type="submit" form="create-agent" class="btn btn-primary" :disabled="busy">
            <span v-if="busy" class="spinner" />{{ busy ? 'Creating…' : 'Create agent' }}
          </button>
        </template>
      </template>
    </Modal>
  </div>
</template>
