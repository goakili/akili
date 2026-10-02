<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{ name?: string | null; email?: string | null; agent?: boolean; small?: boolean }>()

const initials = computed(() => {
  const src = (props.name || props.email || '?').trim()
  const base = props.name ? src : src.split('@')[0]
  const parts = base.split(/[\s._-]+/).filter(Boolean)
  const s = parts.length >= 2 ? parts[0][0] + parts[1][0] : base.slice(0, 2)
  return s.toUpperCase()
})
</script>

<template>
  <span v-if="agent" class="avatar agent" :class="{ sm: small }" aria-hidden="true"><img src="/logo-icon.svg" alt="" /></span>
  <span v-else class="avatar" :class="{ sm: small }" aria-hidden="true">{{ initials }}</span>
</template>
