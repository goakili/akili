<!--
  Copyright (c) 2026 Jonas Kaninda.
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api, type AutonomySettings } from '../../api'
import { useToast } from '../../stores/toast'

const toast = useToast()
const form = ref<AutonomySettings | null>(null)
const saving = ref(false)

const FIELDS: { key: keyof AutonomySettings; label: string; hint: string; max?: number }[] = [
  { key: 'grant_max_hours', label: 'Longest grant (hours)', hint: 'At most 168 (7 days).', max: 168 },
  { key: 'stop_repeat_failures', label: 'Pause after the same call fails', hint: 'Times in a row for one identical call.' },
  { key: 'stop_error_streak', label: 'Pause after consecutive failures', hint: 'Any calls, all failing.' },
  { key: 'stop_max_tool_calls', label: 'Pause after tool calls', hint: 'Per task, counted again after each answer.' },
  { key: 'suggest_min_approvals', label: 'Suggest after approvals', hint: 'Of the same call within 30 days.' },
  { key: 'suggest_min_approvers', label: 'Distinct approvers needed', hint: 'So one person cannot grow autonomy alone.' },
  { key: 'suggest_min_days', label: 'Spread over at least (days)', hint: 'Between the first and last approval.' },
]

async function load() {
  try {
    form.value = await api.autonomySettings()
  } catch {
    /* toasted */
  }
}

async function save() {
  if (!form.value) return
  saving.value = true
  try {
    form.value = await api.setAutonomySettings(form.value)
    toast.success('Autonomy settings saved')
  } catch {
    /* toasted */
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <form class="card" style="max-width: 720px" @submit.prevent="save">
    <div class="card-body stack loose">
      <p class="muted" style="margin: 0">
        When a task looks stuck, Akili pauses it and asks an operator instead of letting it loop. Suggestions to grant autonomy need clean, repeated evidence.
      </p>
      <div v-if="!form" class="skel" style="height: 200px" aria-busy="true" />
      <div v-else class="grid-2">
        <div v-for="f in FIELDS" :key="f.key" class="field">
          <label :for="`as-${f.key}`">{{ f.label }}</label>
          <input :id="`as-${f.key}`" v-model.number="form[f.key]" class="input" type="number" min="1" :max="f.max" />
          <span class="hint">{{ f.hint }}</span>
        </div>
      </div>
      <div class="row end"><button type="submit" class="btn btn-primary" :disabled="saving || !form"><span v-if="saving" class="spinner" />Save</button></div>
    </div>
  </form>
</template>
