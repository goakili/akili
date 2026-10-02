<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { ref } from 'vue'
import { copyText } from '../lib/clipboard'
import Icon from './Icon'

const props = defineProps<{ value: string; label?: string }>()
const copied = ref(false)
async function copy() {
  copied.value = await copyText(props.value)
  setTimeout(() => (copied.value = false), 1600)
}
</script>

<template>
  <div class="field">
    <span v-if="label" class="label">{{ label }}</span>
    <div class="copy-field">
      <pre class="code">{{ value }}</pre>
      <button type="button" class="btn btn-sm" :aria-label="`Copy ${label ?? 'value'}`" @click="copy">
        <Icon :name="copied ? 'check' : 'copy'" />{{ copied ? 'Copied' : 'Copy' }}
      </button>
    </div>
  </div>
</template>
