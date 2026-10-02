<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
defineProps<{ title: string; subtitle?: string; back?: { to: string; label: string } }>()
</script>

<template>
  <header class="page-head">
    <div class="ph-text">
      <nav v-if="back" class="crumbs" aria-label="Breadcrumb">
        <RouterLink :to="back.to">{{ back.label }}</RouterLink><span aria-hidden="true">/</span>
      </nav>
      <div class="ph-title">
        <h1>{{ title }}</h1>
        <slot name="badges" />
      </div>
      <p v-if="subtitle || $slots.subtitle" class="sub" style="margin-bottom: 0">
        <slot name="subtitle">{{ subtitle }}</slot>
      </p>
    </div>
    <div v-if="$slots.default" class="ph-actions"><slot /></div>
  </header>
</template>
