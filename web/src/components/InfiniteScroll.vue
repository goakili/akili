<!--
  Copyright (c) 2026 Jonas Kaninda.
  SPDX-License-Identifier: MIT
-->
<script setup lang="ts">
// Asks again after each page while still in view (a short page, or a client-side filter hiding most rows).
import { onMounted, onUnmounted, ref, watch } from 'vue'

const props = defineProps<{ hasMore: boolean; loading: boolean }>()
const emit = defineEmits<{ more: [] }>()

const el = ref<HTMLElement | null>(null)
const visible = ref(false)
let io: IntersectionObserver | null = null

function check() {
  if (visible.value && props.hasMore && !props.loading) emit('more')
}

onMounted(() => {
  io = new IntersectionObserver(
    (entries) => {
      visible.value = entries.some((e) => e.isIntersecting)
      check()
    },
    { rootMargin: '600px 0px' },
  )
  if (el.value) io.observe(el.value)
})
onUnmounted(() => io?.disconnect())
watch(() => [props.hasMore, props.loading], check, { flush: 'post' })
</script>

<template>
  <div ref="el" class="infinite-scroll" :aria-busy="loading">
    <span v-if="loading && hasMore" class="row muted xs" role="status"><span class="spinner" />Loading more…</span>
  </div>
</template>

<style scoped>
.infinite-scroll {
  display: flex;
  justify-content: center;
  min-height: 1px;
  padding: 10px 0;
}
.infinite-scroll:empty {
  padding: 0;
}
</style>
