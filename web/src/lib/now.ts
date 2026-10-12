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
