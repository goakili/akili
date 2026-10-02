<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import Icon, { type IconName } from './Icon'
withDefaults(defineProps<{ title: string; icon?: IconName; compact?: boolean }>(), { icon: 'sparkles', compact: false })
</script>

<template>
  <div class="empty" :class="{ compact }">
    <div v-if="!compact" class="empty-ic" aria-hidden="true"><Icon :name="icon" /></div>
    <strong>{{ title }}</strong>
    <p v-if="$slots.default"><slot /></p>
    <div v-if="$slots.actions" class="empty-actions"><slot name="actions" /></div>
  </div>
</template>
