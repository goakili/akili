<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, type ChatSession } from '../api'
import { useAuth } from '../stores/auth'
import { useCatalog } from '../stores/catalog'
import { useLive } from '../stores/live'
import { num, relTime, usd } from '../lib/format'
import { useNow } from '../lib/now'
import { usePaged } from '../lib/paged'
import Badge from '../components/Badge.vue'
import Modal from '../components/Modal.vue'
import PageHeader from '../components/PageHeader.vue'
import EmptyState from '../components/EmptyState.vue'
import InfiniteScroll from '../components/InfiniteScroll.vue'
import SkeletonRows from '../components/SkeletonRows.vue'
import Icon from '../components/Icon'

const auth = useAuth()
const catalog = useCatalog()
const live = useLive()
const route = useRoute()
const router = useRouter()
const now = useNow()

const mode = ref<'' | 'chat' | 'task'>('')
const list = usePaged((page) => api.pageSessions({ mode: mode.value || undefined, page }))
const { items: sessions, loading, loadingMore, hasMore } = list
const showNew = ref(false)
const newAgent = ref('')
const newTitle = ref('')
const creating = ref(false)

const chatable = computed(() =>
  catalog.agents.filter((a) => a.status !== 'pending' && a.status !== 'revoked').sort((a, b) => Number(b.status === 'online') - Number(a.status === 'online')),
)
const selectedAgent = computed(() => catalog.agents.find((a) => a.id === newAgent.value))

async function openNew() {
  await catalog.loadAgents()
  newAgent.value = chatable.value.find((a) => a.status === 'online')?.id ?? chatable.value[0]?.id ?? ''
  newTitle.value = ''
  showNew.value = true
}

function closeNew() {
  showNew.value = false
  if (route.query.new) router.replace({ query: {} })
}

async function create() {
  if (!newAgent.value) return
  creating.value = true
  try {
    const s = await api.createSession(newAgent.value, newTitle.value.trim())
    showNew.value = false
    router.push(`/sessions/${s.id}`)
  } catch {
    /* toasted */
  } finally {
    creating.value = false
  }
}

function setMode(m: '' | 'chat' | 'task') {
  mode.value = m
  list.reload()
}

let t: ReturnType<typeof setTimeout> | null = null
let off: (() => void) | null = null
const RELEVANT = new Set(['session.created', 'session.closed', 'session.state', 'message'])
onMounted(() => {
  list.reload()
  catalog.loadAgents()
  if (route.query.new && auth.isOperator) openNew()
  off = live.on((ev) => {
    if (!RELEVANT.has(ev.type)) return
    if (ev.type === 'session.state' && ev.session_id) {
      const s = sessions.value.find((x) => x.id === ev.session_id)
      if (s) s.state = (ev.data as { state: ChatSession['state'] }).state
      return
    }
    if (t) clearTimeout(t)
    t = setTimeout(list.refresh, 1000)
  })
})
onUnmounted(() => {
  off?.()
  if (t) clearTimeout(t)
})

const open = (id: string) => router.push(`/sessions/${id}`)
</script>

<template>
  <div>
    <PageHeader title="Chat & sessions" subtitle="Live conversations with agents and the transcripts of task runs.">
      <button v-if="auth.isOperator" type="button" class="btn btn-primary" @click="openNew"><Icon name="plus" />New chat</button>
    </PageHeader>

    <div class="toolbar">
      <div class="segmented" role="group" aria-label="Filter by mode">
        <button type="button" :class="{ on: mode === '' }" :aria-pressed="mode === ''" @click="setMode('')">All</button>
        <button type="button" :class="{ on: mode === 'chat' }" :aria-pressed="mode === 'chat'" @click="setMode('chat')"><Icon name="chat" />Chats</button>
        <button type="button" :class="{ on: mode === 'task' }" :aria-pressed="mode === 'task'" @click="setMode('task')"><Icon name="tasks" />Task runs</button>
      </div>
    </div>

    <div class="card">
      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr><th>Session</th><th>Agent</th><th class="hide-mobile">Mode</th><th>State</th><th class="right hide-mobile">Tokens</th><th class="right hide-mobile">Cost</th><th class="hide-mobile">Last activity</th></tr>
          </thead>
          <tbody>
            <SkeletonRows v-if="loading" :cols="7" :rows="5" />
            <tr v-else-if="!sessions.length">
              <td colspan="7">
                <EmptyState title="No sessions yet" icon="chat">
                  Start a chat with an online agent, or run a task: its transcript shows up here.
                  <template v-if="auth.isOperator" #actions><button type="button" class="btn btn-primary" @click="openNew"><Icon name="plus" />New chat</button></template>
                </EmptyState>
              </td>
            </tr>
            <tr v-for="s in loading ? [] : sessions" :key="s.id" class="clickable" tabindex="0" @click="open(s.id)" @keydown.enter="open(s.id)">
              <td>
                <RouterLink :to="`/sessions/${s.id}`" class="cell-title" @click.stop>{{ s.title || (s.mode === 'task' ? 'Task run' : 'Chat') }}</RouterLink>
                <div class="cell-sub">{{ relTime(s.created_at, now) }}</div>
              </td>
              <td class="nowrap">
                <span class="row" style="gap: 6px">
                  <span class="status-dot" :class="catalog.agents.find((a) => a.id === s.agent_id)?.status" aria-hidden="true" />{{ catalog.agentName(s.agent_id) }}
                </span>
              </td>
              <td class="hide-mobile"><Badge :value="s.mode" /></td>
              <td><Badge :value="s.status === 'closed' ? 'closed' : s.state || 'idle'" /></td>
              <td class="right num hide-mobile">{{ num(s.input_tokens + s.output_tokens) }}</td>
              <td class="right num hide-mobile">{{ usd(s.cost_usd) }}</td>
              <td class="nowrap hide-mobile">{{ relTime(s.last_activity_at ?? s.updated_at, now) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <InfiniteScroll :has-more="hasMore" :loading="loadingMore" @more="list.more" />
    </div>

    <Modal :open="showNew" title="New chat" @close="closeNew">
      <form id="new-chat" class="stack" @submit.prevent="create">
        <div class="field">
          <label for="nc-agent">Agent</label>
          <select id="nc-agent" v-model="newAgent" class="select" required>
            <option value="" disabled>Select an agent</option>
            <option v-for="a in chatable" :key="a.id" :value="a.id">{{ a.name }} · {{ a.status }}</option>
          </select>
          <span v-if="!chatable.length" class="hint">No enrolled agents yet. <RouterLink to="/agents">Add one first.</RouterLink></span>
          <span v-else-if="selectedAgent && selectedAgent.status !== 'online'" class="hint warn">
            {{ selectedAgent.name }} is {{ selectedAgent.status }}: messages wait until it reconnects.
          </span>
        </div>
        <div class="field">
          <label for="nc-title">Title <span class="opt">(optional)</span></label>
          <input id="nc-title" v-model="newTitle" class="input" maxlength="200" placeholder="e.g. Disk usage on web-01" />
        </div>
      </form>
      <template #footer>
        <button type="button" class="btn" @click="closeNew">Cancel</button>
        <button type="submit" form="new-chat" class="btn btn-primary" :disabled="!newAgent || creating">
          <span v-if="creating" class="spinner" />Start chat
        </button>
      </template>
    </Modal>
  </div>
</template>
