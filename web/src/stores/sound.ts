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
import { computed, ref, watch } from 'vue'
import { useAuth } from './auth'
import { useLive } from './live'
import { audioRunning, playChime, unlockAudio } from '../lib/chime'
import { COALESCE_MS, parseSoundPrefs, soundFor, type SoundKind, type SoundPrefs } from '../lib/sounds'

const PREFS_KEY = 'akili.sound'
// A visible tab claims a sound first; background tabs wait so they only play when no tab is visible.
const HIDDEN_DELAY_MS = 250

function readPrefs(): SoundPrefs {
  try {
    return parseSoundPrefs(window.localStorage.getItem(PREFS_KEY))
  } catch {
    return parseSoundPrefs(null)
  }
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

/** Sounds for approval requests and finished tasks, played by one tab only. */
export const useSound = defineStore('sound', () => {
  const prefs = ref<SoundPrefs>(readPrefs())
  const unlocked = ref(false)
  const announced = new Set<string>()
  const lastPlayed = new Map<SoundKind, number>()
  let off: (() => void) | null = null

  watch(
    prefs,
    (p) => {
      try {
        window.localStorage.setItem(PREFS_KEY, JSON.stringify(p))
      } catch {
        /* applies to this tab only */
      }
    },
    { deep: true },
  )

  /** Sound is on but the browser has not allowed audio yet (no click or key press so far). */
  const blocked = computed(() => prefs.value.enabled && !unlocked.value)

  async function unlock() {
    unlocked.value = await unlockAudio()
    if (unlocked.value) removeGestureListeners()
  }
  const onGesture = () => void unlock()
  function removeGestureListeners() {
    window.removeEventListener('pointerdown', onGesture, true)
    window.removeEventListener('keydown', onGesture, true)
  }

  // Web Locks make the claim atomic across tabs, and holding the lock for the window coalesces bursts.
  async function announce(kind: SoundKind) {
    if (!audioRunning()) return
    if (document.visibilityState !== 'visible') await sleep(HIDDEN_DELAY_MS)
    const locks = typeof navigator !== 'undefined' ? navigator.locks : undefined
    if (!locks) {
      const now = Date.now()
      if (now - (lastPlayed.get(kind) ?? 0) < COALESCE_MS) return
      lastPlayed.set(kind, now)
      playChime(kind, prefs.value.volume)
      return
    }
    await locks.request(`akili.sound.${kind}`, { ifAvailable: true }, async (lock) => {
      if (!lock) return
      playChime(kind, prefs.value.volume)
      await sleep(COALESCE_MS)
    })
  }

  function start() {
    if (off) return
    const auth = useAuth()
    const live = useLive()
    window.addEventListener('pointerdown', onGesture, true)
    window.addEventListener('keydown', onGesture, true)
    off = live.on((ev) => {
      const kind = soundFor(ev, { userId: auth.user?.id ?? null, canApprove: auth.isOperator, prefs: prefs.value, announced })
      if (kind) void announce(kind)
    })
  }

  function stop() {
    off?.()
    off = null
    removeGestureListeners()
  }

  /** Plays a sound now, for the settings menu (a click, so it also unlocks audio). */
  async function test(kind: SoundKind) {
    await unlock()
    playChime(kind, prefs.value.volume)
  }

  return { prefs, blocked, start, stop, test, unlock }
})
