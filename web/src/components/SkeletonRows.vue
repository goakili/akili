<!--
  Copyright (c) 2026 Jonas Kaninda.
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
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
