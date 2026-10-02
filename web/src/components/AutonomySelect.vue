<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed } from 'vue'
import { AUTONOMY_LEVELS, type Autonomy } from '../api'

const model = defineModel<Autonomy>({ required: true })
defineProps<{ id: string; disabled?: boolean }>()
const help = computed(() => AUTONOMY_LEVELS.find((l) => l.value === model.value)?.help ?? '')
</script>

<template>
  <div class="field">
    <label :for="id">Autonomy</label>
    <select :id="id" v-model.number="model" class="select" :disabled="disabled" :aria-describedby="`${id}-help`">
      <option v-for="l in AUTONOMY_LEVELS" :key="l.value" :value="l.value">{{ l.label }}</option>
    </select>
    <span :id="`${id}-help`" class="hint">{{ help }} Anything above goes to a human; critical actions always need approval.</span>
  </div>
</template>
