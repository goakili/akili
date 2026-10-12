<!--
  Copyright (c) 2026 Jonas Kaninda.

  This file is part of Akili.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published by
  the Free Software Foundation, either version 3 of the License, or
  (at your option) any later version.

  This program is distributed in the hope that it will be useful,
  but WITHOUT ANY WARRANTY; without even the implied warranty of
  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
  GNU Affero General Public License for more details.

  You should have received a copy of the GNU Affero General Public License
  along with this program.  If not, see <https://www.gnu.org/licenses/>.

  SPDX-License-Identifier: AGPL-3.0-or-later
-->
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
