<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
// Marks a task an alert or a Miabi event created (trigger_ref "alert:<route>:<fingerprint>",
// "miabi:<watch>:verify:<ref>" or "miabi:<watch>:<event>") and links to the route or watch. A chat
// task's trigger_ref is a conversation id, so it has nothing to link to.
import { computed } from 'vue'
import type { Task } from '../api'
import { useAuth } from '../stores/auth'
import Icon from './Icon'

const props = defineProps<{ task: Pick<Task, 'trigger' | 'trigger_ref'> }>()
const auth = useAuth()
const alert = computed(() => {
  const m = /^alert:([^:]+):(.*)$/.exec(props.task.trigger_ref ?? '')
  return m ? { route: m[1], fingerprint: m[2] } : null
})
const alertTitle = computed(() => `Created by an alert${alert.value?.fingerprint ? ` (fingerprint ${alert.value.fingerprint})` : ''}`)

const miabi = computed(() => {
  const m = /^miabi:([^:]+):(.*)$/.exec(props.task.trigger_ref ?? '')
  return m ? { watch: m[1], verify: m[2].startsWith('verify'), event: m[2] } : null
})
const miabiTitle = computed(() => {
  const m = miabi.value
  if (!m) return 'Created by a Miabi event'
  return m.verify ? 'Verifies a successful Miabi deploy' : `Triages a Miabi ${m.event} event`
})
</script>

<template>
  <template v-if="task.trigger === 'alert'">
    <RouterLink v-if="auth.isAdmin && alert" :to="{ path: '/alerts', query: { route: alert.route } }" class="badge warn trigger-chip" :title="`${alertTitle}: open the alert route`" @click.stop>
      <Icon name="bell" />alert
    </RouterLink>
    <span v-else class="badge warn trigger-chip" :title="alertTitle"><Icon name="bell" />alert</span>
  </template>
  <template v-else-if="task.trigger === 'miabi'">
    <RouterLink v-if="miabi" :to="{ path: '/miabi', query: { watch: miabi.watch } }" class="badge accent trigger-chip" :title="`${miabiTitle}: open the watch`" @click.stop>
      <Icon name="layers" />Miabi{{ miabi.verify ? ' · verify' : '' }}
    </RouterLink>
    <span v-else class="badge accent trigger-chip" :title="miabiTitle"><Icon name="layers" />Miabi</span>
  </template>
  <span v-else-if="task.trigger === 'chat'" class="badge info trigger-chip" title="Created from a chat conversation (Slack, Telegram or Signal)"><Icon name="messages" />Chat</span>
</template>

<style scoped>
.trigger-chip {
  text-decoration: none;
}
</style>
