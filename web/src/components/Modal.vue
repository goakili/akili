<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import Icon from './Icon'

const props = withDefaults(defineProps<{ open: boolean; title: string; wide?: boolean; dismissable?: boolean }>(), {
  wide: false,
  dismissable: true,
})
const emit = defineEmits<{ (e: 'close'): void }>()
const dlg = ref<HTMLDialogElement | null>(null)
const titleId = `dlg-${Math.random().toString(36).slice(2, 9)}`

async function sync() {
  const d = dlg.value
  if (!d) return
  if (props.open && !d.open) {
    d.showModal()
    await nextTick()
    const first = d.querySelector<HTMLElement>('[autofocus], .modal-body input:not([type=hidden]), .modal-body textarea, .modal-body select')
    first?.focus()
  } else if (!props.open && d.open) {
    d.close()
  }
}

watch(() => props.open, sync)
onMounted(sync)
onBeforeUnmount(() => dlg.value?.open && dlg.value.close())

function onCancel(e: Event) {
  e.preventDefault()
  if (props.dismissable) emit('close')
}
function onBackdrop(e: MouseEvent) {
  if (props.dismissable && e.target === dlg.value) emit('close')
}
</script>

<template>
  <dialog ref="dlg" class="modal" :class="{ wide }" :aria-labelledby="titleId" @cancel="onCancel" @mousedown="onBackdrop">
    <div v-if="open" class="modal-inner">
      <div class="modal-head">
        <h2 :id="titleId">{{ title }}</h2>
        <button v-if="dismissable" type="button" class="btn btn-ghost btn-icon btn-sm" aria-label="Close" @click="emit('close')">
          <Icon name="x" />
        </button>
      </div>
      <div class="modal-body"><slot /></div>
      <div v-if="$slots.footer" class="modal-foot"><slot name="footer" /></div>
    </div>
  </dialog>
</template>
