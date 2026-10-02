// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

import { defineStore } from 'pinia'
import { ref } from 'vue'

export type ToastKind = 'error' | 'success' | 'info' | 'warn'

export interface Toast {
  id: number
  kind: ToastKind
  message: string
}

let seq = 0

export const useToast = defineStore('toast', () => {
  const items = ref<Toast[]>([])

  function push(kind: ToastKind, message: string, ttl = kind === 'error' ? 7000 : 3500) {
    // Collapse identical messages fired in a burst (e.g. several failing requests at once).
    if (items.value.some((t) => t.kind === kind && t.message === message)) return
    const id = ++seq
    items.value.push({ id, kind, message })
    if (items.value.length > 5) items.value.shift()
    setTimeout(() => dismiss(id), ttl)
  }

  function dismiss(id: number) {
    items.value = items.value.filter((t) => t.id !== id)
  }

  return {
    items,
    dismiss,
    error: (m: string) => push('error', m),
    success: (m: string) => push('success', m),
    info: (m: string) => push('info', m),
    warn: (m: string) => push('warn', m),
  }
})
