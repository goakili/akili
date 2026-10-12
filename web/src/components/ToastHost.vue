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
