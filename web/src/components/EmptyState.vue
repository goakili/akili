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
