// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// asciinema v2 casts: a JSON header line, then one [time, "o"|"i", data] event per line.

export interface CastHeader {
  version: number
  width: number
  height: number
  timestamp?: number
  title?: string
}

export interface CastEvent {
  /** Seconds since the start, with long idle gaps shortened (see IDLE_CAP). */
  t: number
  /** The recorded time, before shortening. */
  real: number
  kind: 'o' | 'i' | string
  data: string
}

export interface Cast {
  header: CastHeader
  events: CastEvent[]
  duration: number
  /** How many idle gaps were shortened. */
  compressed: number
}

/** Idle gaps longer than this play back as this long (like asciinema's idle_time_limit). */
export const IDLE_CAP = 2

export function parseCast(text: string): Cast {
  const lines = text.split('\n').filter((l) => l.trim())
  if (!lines.length) throw new Error('The recording is empty.')
  let header: CastHeader
  try {
    header = JSON.parse(lines[0]) as CastHeader
  } catch {
    throw new Error('The recording is not an asciinema cast.')
  }
  if (header.version !== 2) throw new Error(`Unsupported recording version ${String(header.version)}.`)
  const events: CastEvent[] = []
  let prevReal = 0
  let t = 0
  let compressed = 0
  for (const line of lines.slice(1)) {
    let e: unknown
    try {
      e = JSON.parse(line)
    } catch {
      continue // a truncated last line
    }
    if (!Array.isArray(e) || e.length < 3 || typeof e[0] !== 'number') continue
    const real = e[0]
    let gap = Math.max(0, real - prevReal)
    if (gap > IDLE_CAP) {
      gap = IDLE_CAP
      compressed++
    }
    t += gap
    prevReal = real
    events.push({ t, real, kind: String(e[1]), data: String(e[2] ?? '') })
  }
  return { header, events, duration: events.length ? events[events.length - 1].t : 0, compressed }
}

/** Keystrokes shown readably: Enter as ⏎, control keys as ^C, escapes as ⎋. */
export function visibleKeys(s: string): string {
  let out = ''
  for (const ch of s) {
    const c = ch.charCodeAt(0)
    if (ch === '\r' || ch === '\n') out += '⏎'
    else if (ch === '\t') out += '⇥'
    else if (ch === '\x7f') out += '⌫'
    else if (ch === '\x1b') out += '⎋'
    else if (c < 32) out += '^' + String.fromCharCode(c + 64)
    else out += ch
  }
  return out
}

export interface InputLine {
  t: number
  text: string
}

/** Groups single keystrokes into lines: a new entry after Enter or a pause of a second. */
export function inputLines(events: CastEvent[]): InputLine[] {
  const out: InputLine[] = []
  let cur: InputLine | null = null
  let last = -Infinity
  for (const e of events) {
    if (e.kind !== 'i') continue
    if (!cur || e.real - last > 1) {
      cur = { t: e.t, text: '' }
      out.push(cur)
    }
    cur.text += visibleKeys(e.data)
    last = e.real
    if (/[\r\n]/.test(e.data)) cur = null
  }
  return out
}
