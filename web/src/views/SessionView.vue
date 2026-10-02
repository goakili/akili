<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import type { ChatSession } from '../api'
import { useCatalog } from '../stores/catalog'
import { useUi } from '../stores/ui'
import { fmtDate } from '../lib/format'
import Badge from '../components/Badge.vue'
import EmptyState from '../components/EmptyState.vue'
import SessionTranscript from '../components/SessionTranscript.vue'

const props = defineProps<{ id: string }>()
const catalog = useCatalog()
const ui = useUi()
const session = ref<ChatSession | null>(null)
const notFound = ref(false)
onMounted(() => catalog.loadAgents())

function onSession(s: ChatSession) {
  session.value = s
  ui.crumb = s.title || (s.mode === 'task' ? 'Task run' : `Chat with ${catalog.agentName(s.agent_id)}`)
}
</script>

<template>
  <EmptyState v-if="notFound" title="Session not found" icon="chat">
    It may have been removed, or the link is wrong.
    <template #actions><RouterLink to="/sessions" class="btn">Back to sessions</RouterLink></template>
  </EmptyState>
  <div v-else class="session-page">
    <header class="page-head" style="margin-bottom: 14px; align-items: center">
      <div class="ph-text">
        <div class="ph-title">
          <h1 style="font-size: 19px">{{ session?.title || (session?.mode === 'task' ? 'Task run' : 'Chat') }}</h1>
          <Badge v-if="session" :value="session.mode" />
        </div>
        <p v-if="session" class="sub small" style="margin: 4px 0 0">
          Started {{ fmtDate(session.created_at) }}
          <template v-if="session.task_id"> · <RouterLink :to="`/tasks/${session.task_id}`">Open task</RouterLink></template>
        </p>
      </div>
    </header>
    <SessionTranscript :session-id="props.id" class="session-frame" @session="onSession" @notfound="notFound = true" />
  </div>
</template>

<style scoped>
.session-page {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
}
.session-frame {
  height: calc(100vh - var(--sticky-top) - 120px);
  min-height: 420px;
}
@media (max-width: 900px) {
  .session-frame {
    height: calc(100dvh - var(--sticky-top) - 100px);
  }
}
</style>
