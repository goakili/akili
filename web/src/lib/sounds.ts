// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

import type { BusEvent, Task } from '../api'

export type SoundKind = 'approval' | 'success' | 'failure'

export interface SoundPrefs {
  enabled: boolean
  volume: number // 0..1
  approvals: boolean
  myTasks: boolean
  allTasks: boolean
}

export const DEFAULT_SOUND_PREFS: SoundPrefs = { enabled: true, volume: 0.6, approvals: true, myTasks: true, allTasks: false }

/** Same kind of sound at most once per window, across every open tab. */
export const COALESCE_MS = 2000

// A task updated again long after it ended (PR status, a late edit) must not chime on every load.
const RECENT_FINISH_MS = 60_000
const MAX_ANNOUNCED = 500

export interface SoundContext {
  userId: string | null
  canApprove: boolean
  prefs: SoundPrefs
  /** Outcomes already announced by this tab; soundFor adds to it. */
  announced: Set<string>
}

/** Decides which sound, if any, an org event deserves. */
export function soundFor(ev: BusEvent, c: SoundContext): SoundKind | null {
  if (!c.prefs.enabled) return null
  if (ev.type === 'approval.created') return c.prefs.approvals && c.canApprove ? 'approval' : null
  if (ev.type !== 'task.updated') return null
  const t = ev.data as Partial<Task> | undefined
  if (!t?.id) return null
  const kind: SoundKind | null = t.status === 'succeeded' ? 'success' : t.status === 'failed' || t.status === 'timed_out' ? 'failure' : null
  if (!kind) return null
  const mine = !!c.userId && t.created_by === c.userId
  if (!c.prefs.allTasks && !(c.prefs.myTasks && mine)) return null
  if (t.finished_at && ev.ts && Date.parse(ev.ts) - Date.parse(t.finished_at) > RECENT_FINISH_MS) return null
  const key = `${t.id}:${t.status}:${t.attempts ?? 0}`
  if (c.announced.has(key)) return null
  if (c.announced.size >= MAX_ANNOUNCED) c.announced.clear()
  c.announced.add(key)
  return kind
}

/** Reads stored preferences, keeping defaults for anything missing or malformed. */
export function parseSoundPrefs(raw: string | null): SoundPrefs {
  const out = { ...DEFAULT_SOUND_PREFS }
  if (!raw) return out
  try {
    const v = JSON.parse(raw) as Partial<Record<keyof SoundPrefs, unknown>>
    for (const k of ['enabled', 'approvals', 'myTasks', 'allTasks'] as const) {
      if (typeof v[k] === 'boolean') out[k] = v[k]
    }
    if (typeof v.volume === 'number' && Number.isFinite(v.volume)) out.volume = Math.min(1, Math.max(0, v.volume))
  } catch {
    /* defaults */
  }
  return out
}
