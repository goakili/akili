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
