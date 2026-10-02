// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

import { ref, onScopeDispose, getCurrentScope } from 'vue'

const now = ref(Date.now())
let users = 0
let timer: ReturnType<typeof setInterval> | null = null

/** A shared clock ref ticking once a second while anything uses it. */
export function useNow() {
  users++
  if (!timer) timer = setInterval(() => (now.value = Date.now()), 1000)
  if (getCurrentScope()) {
    onScopeDispose(() => {
      users--
      if (users <= 0 && timer) {
        clearInterval(timer)
        timer = null
        users = 0
      }
    })
  }
  return now
}
