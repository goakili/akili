<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
// An agent's ask_user question: pick one of its options or answer in your own words. The options are
// model text, shown as plain text; answering never approves a tool call.
import { computed, ref } from 'vue'
import { api, type Question } from '../api'
import { useAuth } from '../stores/auth'
import { useCatalog } from '../stores/catalog'
import { useToast } from '../stores/toast'
import { countdown, fmtDate, relTime } from '../lib/format'
import { useNow } from '../lib/now'
import Badge from './Badge.vue'
import Icon from './Icon'

const props = defineProps<{ question: Question }>()
const emit = defineEmits<{ (e: 'resolved', q: Question): void }>()

const auth = useAuth()
const catalog = useCatalog()
const toast = useToast()
const now = useNow()
const busy = ref(false)
const own = ref('')
const writing = ref(false)
const ownId = `own-${Math.random().toString(36).slice(2, 8)}`

const pending = computed(() => props.question.status === 'pending')
const left = computed(() => countdown(props.question.expires_at, now.value))
const canAnswer = computed(() => pending.value && auth.isOperator && left.value !== 'expired' && !busy.value)

async function answer(a: { choice: number } | { text: string }) {
  if (!canAnswer.value) return
  busy.value = true
  try {
    emit('resolved', await api.answerQuestion(props.question.id, a))
    toast.success(`Answer sent to ${catalog.agentName(props.question.agent_id)}`)
  } catch {
    /* toasted */
  } finally {
    busy.value = false
  }
}

function sendOwn() {
  const text = own.value.trim()
  if (text) answer({ text })
}
</script>

<template>
  <div class="question-card" :class="{ resolved: !pending }" role="group" :aria-label="`Question from ${catalog.agentName(question.agent_id)}`">
    <div class="row wrap between">
      <div class="qc-title">
        <Icon name="help" class="mark" />
        <strong>{{ pending ? 'The agent needs your decision' : 'Question' }}</strong>
        <Badge v-if="!pending" :value="question.status" />
      </div>
      <span v-if="pending" class="countdown" :title="`Expires ${fmtDate(question.expires_at)}`"><Icon name="clock" />{{ left === 'expired' ? 'expired' : `expires in ${left}` }}</span>
    </div>
    <p class="qc-question">{{ question.question }}</p>

    <div class="qc-options" role="list">
      <button
        v-for="(o, i) in question.options ?? []"
        :key="i"
        type="button"
        role="listitem"
        class="qc-option"
        :class="{ chosen: question.choice === i, recommended: o.recommended }"
        :disabled="!canAnswer"
        :aria-pressed="question.choice === i"
        @click="answer({ choice: i })"
      >
        <span class="qc-label">{{ o.label }}<span v-if="o.recommended" class="qc-rec">Recommended</span></span>
        <span v-if="o.description" class="qc-desc">{{ o.description }}</span>
      </button>
    </div>

    <template v-if="pending && auth.isOperator">
      <button v-if="!writing" type="button" class="btn btn-sm btn-ghost qc-else" :disabled="!canAnswer" @click="writing = true"><Icon name="edit" />Something else…</button>
      <form v-else class="stack tight" @submit.prevent="sendOwn">
        <label :for="ownId" class="small strong">Your answer</label>
        <textarea :id="ownId" v-model="own" class="input" rows="3" maxlength="4000" placeholder="Describe what you want instead" @keydown.meta.enter="sendOwn" @keydown.ctrl.enter="sendOwn" />
        <div class="row" style="justify-content: flex-end">
          <button type="button" class="btn btn-sm" @click="writing = false">Cancel</button>
          <button type="submit" class="btn btn-sm btn-primary" :disabled="!canAnswer || !own.trim()"><span v-if="busy" class="spinner" /><Icon v-else name="send" />Send answer</button>
        </div>
      </form>
    </template>
    <div v-else-if="pending" class="small muted">Operators can answer this question.</div>

    <div v-if="question.status === 'answered'" class="small muted">
      <template v-if="question.choice === null">Answered in their own words: “{{ question.answer }}”</template>
      <template v-else>Chose “{{ question.answer }}”</template>
      {{ question.answered_at ? relTime(question.answered_at, now) : '' }}
    </div>
    <div v-else-if="question.status === 'expired'" class="small muted">
      Nobody answered in time; the agent {{ question.options?.some((o) => o.recommended) ? 'was told to use its recommended option only if that is safe and easy to undo, or stop' : 'was told to stop' }}.
    </div>
  </div>
</template>

<style scoped>
.question-card {
  border: 1px solid color-mix(in srgb, var(--primary-500) 40%, transparent);
  border-left: 3px solid var(--primary-500);
  background: var(--bg-primary);
  box-shadow: var(--shadow-sm);
  border-radius: var(--radius-lg);
  padding: 14px 16px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.question-card.resolved {
  background: var(--bg-secondary);
  border-color: var(--border-primary);
  border-left-color: var(--border-input);
  box-shadow: none;
}
.qc-title {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.qc-title .mark {
  width: 18px;
  height: 18px;
  color: var(--primary-500);
}
.qc-question {
  margin: 0;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.qc-options {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(220px, 100%), 1fr));
  gap: 8px;
}
.qc-option {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 2px;
  text-align: left;
  padding: 10px 12px;
  border: 1px solid var(--border-input);
  border-radius: var(--radius);
  background: var(--bg-primary);
  color: var(--text-primary);
  cursor: pointer;
  font: inherit;
  transition:
    border-color var(--t) var(--ease),
    background var(--t) var(--ease);
}
.qc-option:hover:not(:disabled) {
  border-color: var(--primary-500);
  background: var(--bg-secondary);
}
.qc-option:focus-visible {
  box-shadow: var(--shadow-focus);
  outline: none;
}
.qc-option:disabled {
  cursor: default;
  opacity: 0.75;
}
.qc-option.chosen {
  border-color: var(--primary-500);
  opacity: 1;
}
.qc-label {
  font-weight: 600;
  overflow-wrap: anywhere;
}
.qc-option.recommended {
  border-color: color-mix(in srgb, var(--primary-500) 55%, var(--border-input));
}
.qc-rec {
  margin-left: 8px;
  padding: 1px 6px;
  border-radius: 999px;
  font-size: 11px;
  font-weight: 600;
  vertical-align: 1px;
  color: var(--primary-500);
  background: color-mix(in srgb, var(--primary-500) 12%, transparent);
}
.qc-desc {
  font-size: 12.5px;
  color: var(--text-secondary);
  overflow-wrap: anywhere;
}
.qc-else {
  align-self: flex-start;
}
</style>
