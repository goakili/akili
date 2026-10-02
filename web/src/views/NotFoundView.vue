<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import EmptyState from '../components/EmptyState.vue'
</script>

<template>
  <div class="card" style="margin-top: 40px">
    <EmptyState title="Page not found" icon="search">
      The page you are looking for does not exist or was moved.
      <template #actions><RouterLink to="/" class="btn btn-primary">Back to dashboard</RouterLink></template>
    </EmptyState>
  </div>
</template>
