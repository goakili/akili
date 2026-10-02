// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

import { defineStore } from 'pinia'
import { ref } from 'vue'

export interface ConfirmOptions {
  title: string
  message?: string
  confirmText?: string
  danger?: boolean
  /** When set, the operator must type this exact text to confirm. */
  requireText?: string
}

interface Pending extends ConfirmOptions {
  resolve: (ok: boolean) => void
}

export const useConfirm = defineStore('confirm', () => {
  const current = ref<Pending | null>(null)

  function ask(opts: ConfirmOptions): Promise<boolean> {
    current.value?.resolve(false)
    return new Promise((resolve) => {
      current.value = { ...opts, resolve }
    })
  }

  function answer(ok: boolean) {
    const c = current.value
    current.value = null
    c?.resolve(ok)
  }

  return { current, ask, answer }
})
