// Copyright (c) 2026 Jonas Kaninda.
// SPDX-License-Identifier: MIT
import type { Constraint } from '../api'

export type ArgMode = 'equals' | 'glob' | 'one_of' | 'range' | 'any'

export interface ArgRow {
  key: string
  mode: ArgMode
  value: string
  min: string
  max: string
}

/** One row per top-level argument, each pinned to the value it had: a grant starts as narrow as the call it came from. */
export function argRows(input: unknown): ArgRow[] {
  if (!input || typeof input !== 'object' || Array.isArray(input)) return []
  return Object.entries(input as Record<string, unknown>).map(([key, v]) => ({
    key,
    mode: 'equals',
    value: typeof v === 'string' ? v : JSON.stringify(v),
    min: typeof v === 'number' ? String(v) : '',
    max: typeof v === 'number' ? String(v) : '',
  }))
}

function parseValue(s: string): unknown {
  try {
    return JSON.parse(s)
  } catch {
    return s
  }
}

export function rowConstraint(r: ArgRow, original?: unknown): Constraint | null {
  switch (r.mode) {
    case 'equals':
      return { equals: typeof original === 'string' ? r.value : parseValue(r.value) }
    case 'glob':
      return r.value.trim() ? { glob: r.value.trim() } : null
    case 'any':
      return { glob: '*' }
    case 'one_of': {
      const list = r.value.split(',').map((v) => v.trim()).filter(Boolean)
      return list.length ? { one_of: list } : null
    }
    case 'range': {
      const c: Constraint = {}
      if (r.min.trim() !== '') c.min = Number(r.min)
      if (r.max.trim() !== '') c.max = Number(r.max)
      return c.min === undefined && c.max === undefined ? null : c
    }
  }
}

export function toMatch(rows: ArgRow[], input: unknown): Record<string, Constraint> | null {
  const orig = (input && typeof input === 'object' ? input : {}) as Record<string, unknown>
  const m: Record<string, Constraint> = {}
  for (const r of rows) {
    const c = rowConstraint(r, orig[r.key])
    if (!c) return null
    m[r.key] = c
  }
  return m
}

export function describeConstraint(c: Constraint): string {
  if (c.equals !== undefined) return `= ${JSON.stringify(c.equals)}`
  if (c.glob === '*') return 'any value'
  if (c.glob) return `matches ${c.glob}`
  if (c.one_of?.length) return `one of ${c.one_of.join(', ')}`
  if (c.min !== undefined && c.max !== undefined) return `${c.min} to ${c.max}`
  if (c.min !== undefined) return `at least ${c.min}`
  if (c.max !== undefined) return `at most ${c.max}`
  return '—'
}

export function describeMatch(m: Record<string, Constraint> | null | undefined): string {
  const e = Object.entries(m ?? {})
  return e.length ? e.map(([k, c]) => `${k} ${describeConstraint(c)}`).join(' · ') : 'no arguments'
}

export const GRANT_DURATIONS: [number, string][] = [
  [1, '1 hour'],
  [2, '2 hours'],
  [4, '4 hours'],
  [8, '8 hours'],
  [24, '1 day'],
  [72, '3 days'],
  [168, '7 days'],
]
