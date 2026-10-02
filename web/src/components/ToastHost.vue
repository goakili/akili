<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { useToast, type ToastKind } from '../stores/toast'
import Icon, { type IconName } from './Icon'
const toast = useToast()
const ICONS: Record<ToastKind, IconName> = { error: 'xCircle', success: 'checkCircle', info: 'info', warn: 'alert' }
</script>

<template>
  <div class="toasts" role="region" aria-live="polite" aria-label="Notifications">
    <div v-for="t in toast.items" :key="t.id" class="toast" :class="t.kind" :role="t.kind === 'error' ? 'alert' : 'status'">
      <span class="t-ic" aria-hidden="true"><Icon :name="ICONS[t.kind]" /></span>
      <span class="msg">{{ t.message }}</span>
      <button type="button" class="btn btn-ghost btn-icon btn-xs" aria-label="Dismiss notification" @click="toast.dismiss(t.id)">
        <Icon name="x" />
      </button>
    </div>
  </div>
</template>
