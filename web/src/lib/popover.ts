// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

import { onBeforeUnmount, ref, watch, type Ref } from 'vue'

/**
 * Open/close state for a dropdown anchored in `root`: closes on outside click and on Escape (returning
 * focus to the trigger), so menus are keyboard reachable and dismissable.
 */
export function usePopover(root: Ref<HTMLElement | null>, trigger?: Ref<HTMLElement | null>) {
  const open = ref(false)

  function onDoc(e: MouseEvent) {
    if (root.value && !root.value.contains(e.target as Node)) open.value = false
  }
  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape' && open.value) {
      open.value = false
      trigger?.value?.focus()
    }
  }

  watch(open, (o) => {
    if (o) {
      document.addEventListener('mousedown', onDoc)
      document.addEventListener('keydown', onKey)
    } else {
      document.removeEventListener('mousedown', onDoc)
      document.removeEventListener('keydown', onKey)
    }
  })
  onBeforeUnmount(() => {
    document.removeEventListener('mousedown', onDoc)
    document.removeEventListener('keydown', onKey)
  })

  return { open, toggle: () => (open.value = !open.value), close: () => (open.value = false) }
}

/** Arrow-key navigation between the [role=menuitem] elements of a menu. */
export function menuKeys(e: KeyboardEvent) {
  if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return
  const menu = e.currentTarget as HTMLElement
  const items = [...menu.querySelectorAll<HTMLElement>('[role=menuitem]')]
  if (!items.length) return
  e.preventDefault()
  const i = items.indexOf(document.activeElement as HTMLElement)
  const j = e.key === 'ArrowDown' ? (i + 1) % items.length : (i - 1 + items.length) % items.length
  items[j].focus()
}
