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
