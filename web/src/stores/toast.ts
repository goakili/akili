// Copyright (c) 2026 Jonas Kaninda.
//
// This file is part of Akili.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.
//
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
