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
// Placeholder table rows for a first load (instead of a spinner).
withDefaults(defineProps<{ rows?: number; cols: number }>(), { rows: 5 })
const widths = ['62%', '45%', '78%', '38%', '55%', '70%']
</script>

<template>
  <tr v-for="r in rows" :key="r" aria-hidden="true">
    <td v-for="c in cols" :key="c">
      <span class="skel" :style="{ width: widths[(r + c) % widths.length] }" />
    </td>
  </tr>
</template>
