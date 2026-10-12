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
