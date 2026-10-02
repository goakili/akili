<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import PageHeader from '../components/PageHeader.vue'
import AuditTab from './settings/AuditTab.vue'
</script>

<template>
  <div>
    <PageHeader title="Audit log" subtitle="Every privileged action and every tool decision, hash-chained so tampering is detectable. Click a row for its details." />
    <AuditTab />
  </div>
</template>
