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

import type { SoundKind } from './sounds'

// Notes are synthesized, not loaded: no audio assets, and each sound stays recognizable at any
// volume. [frequency Hz, start s, duration s]
const NOTES: Record<SoundKind, [number, number, number][]> = {
  approval: [
    [880, 0, 0.16],
    [1318.5, 0.13, 0.3],
  ],
  success: [[1046.5, 0, 0.4]],
  failure: [
    [659.3, 0, 0.18],
    [440, 0.15, 0.34],
  ],
}

let ctx: AudioContext | null = null

/**
 * Creates or resumes the audio context. Browsers only allow this from a user gesture, so it is
 * called on the first click or key press. Resolves to whether audio can now play.
 */
export async function unlockAudio(): Promise<boolean> {
  try {
    ctx ??= new AudioContext()
    if (ctx.state === 'suspended') await ctx.resume()
    return ctx.state === 'running'
  } catch {
    return false
  }
}

export function audioRunning(): boolean {
  return ctx?.state === 'running'
}

/** Plays a sound; false when audio is still blocked by the browser. */
export function playChime(kind: SoundKind, volume: number): boolean {
  if (!ctx || ctx.state !== 'running' || volume <= 0) return false
  const t0 = ctx.currentTime + 0.01
  const out = ctx.createGain()
  out.gain.value = Math.min(1, volume) * 0.3
  out.connect(ctx.destination)
  for (const [freq, start, dur] of NOTES[kind]) {
    const osc = ctx.createOscillator()
    osc.type = kind === 'approval' ? 'triangle' : 'sine'
    osc.frequency.value = freq
    const env = ctx.createGain()
    env.gain.setValueAtTime(0.0001, t0 + start)
    env.gain.exponentialRampToValueAtTime(1, t0 + start + 0.015)
    env.gain.exponentialRampToValueAtTime(0.0001, t0 + start + dur)
    osc.connect(env).connect(out)
    osc.start(t0 + start)
    osc.stop(t0 + start + dur + 0.02)
  }
  return true
}
