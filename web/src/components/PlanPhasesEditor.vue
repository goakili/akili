<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
// Edits a plan's phase list: titles, optional detail, order. Phases keep their id, so the server
// keeps their status; new rows start as "to do".
import { nextTick, ref } from 'vue'
import type { PlanPhaseInput } from '../api'
import Icon from './Icon'

const phases = defineModel<PlanPhaseInput[]>({ required: true })
defineProps<{ idPrefix?: string }>()
const MAX = 100
const titles = ref<HTMLInputElement[]>([])

async function add(at = phases.value.length) {
  if (phases.value.length >= MAX) return
  phases.value.splice(at, 0, { title: '', detail: '', done_when: '' })
  await nextTick()
  titles.value[at]?.focus()
}

function move(i: number, d: -1 | 1) {
  const j = i + d
  if (j < 0 || j >= phases.value.length) return
  const s = phases.value
  ;[s[i], s[j]] = [s[j], s[i]]
}

function remove(i: number) {
  phases.value.splice(i, 1)
}
</script>

<template>
  <div class="phases-editor">
    <ol v-if="phases.length" class="phases-edit-list">
      <li v-for="(s, i) in phases" :key="s.id ?? `new-${i}`" class="phases-edit-row">
        <span class="phases-edit-n" aria-hidden="true">{{ i + 1 }}</span>
        <div class="phases-edit-fields">
          <input
            ref="titles"
            v-model="s.title"
            class="input"
            maxlength="300"
            :aria-label="`Phase ${i + 1}`"
            placeholder="What needs to happen"
            @keydown.enter.prevent="add(i + 1)"
          />
          <textarea v-model="s.detail" class="textarea" rows="1" maxlength="4000" :aria-label="`Phase ${i + 1} detail`" placeholder="Detail (optional)" />
          <input v-model="s.done_when" class="input phases-done-when" maxlength="2000" :aria-label="`Phase ${i + 1}: done when`" placeholder="Done when… (optional), e.g. merged with a test" />
        </div>
        <div class="phases-edit-actions">
          <button type="button" class="btn btn-ghost btn-icon btn-sm" :disabled="i === 0" :aria-label="`Move phase ${i + 1} up`" @click="move(i, -1)"><Icon name="chevronDown" style="transform: rotate(180deg)" /></button>
          <button type="button" class="btn btn-ghost btn-icon btn-sm" :disabled="i === phases.length - 1" :aria-label="`Move phase ${i + 1} down`" @click="move(i, 1)"><Icon name="chevronDown" /></button>
          <button type="button" class="btn btn-ghost btn-icon btn-sm" :aria-label="`Remove phase ${i + 1}`" @click="remove(i)"><Icon name="trash" /></button>
        </div>
      </li>
    </ol>
    <p v-else class="hint" style="margin: 0">No phases yet. Break the work into phases the agent can finish and report on one at a time.</p>
    <button type="button" class="btn btn-sm" :disabled="phases.length >= MAX" @click="add()"><Icon name="plus" />Add phase</button>
  </div>
</template>

<style scoped>
.phases-editor {
  display: flex;
  flex-direction: column;
  gap: 8px;
  align-items: flex-start;
}
.phases-edit-list {
  list-style: none;
  margin: 0;
  padding: 0;
  width: 100%;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.phases-edit-row {
  display: flex;
  gap: 8px;
  align-items: flex-start;
}
.phases-edit-n {
  flex: none;
  width: 22px;
  padding-top: 8px;
  text-align: right;
  font-variant-numeric: tabular-nums;
  color: var(--text-3);
  font-size: 12px;
}
.phases-edit-fields {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.phases-done-when {
  font-size: 12px;
  height: 30px;
}
.phases-edit-fields .textarea {
  min-height: 0;
  font-size: 12px;
  resize: vertical;
}
.phases-edit-actions {
  display: flex;
  gap: 0;
  padding-top: 2px;
}
</style>
