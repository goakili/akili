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
