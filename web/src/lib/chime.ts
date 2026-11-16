// Copyright (c) 2026 Jonas Kaninda.
// SPDX-License-Identifier: Apache-2.0

import type { SoundKind } from './sounds'

// Synthesized, so no audio assets: [frequency Hz, start s, duration s]
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

/** Browsers allow audio only after a user gesture, so this runs on the first click or key press. */
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
