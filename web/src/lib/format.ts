// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

import type { Autonomy } from '../api'

export function fmtDate(s: string | null | undefined): string {
  if (!s) return '—'
  const d = new Date(s)
  if (isNaN(d.getTime()) || d.getFullYear() < 2000) return '—'
  return d.toLocaleString(undefined, { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

export function fmtTime(s: string | null | undefined): string {
  if (!s) return ''
  const d = new Date(s)
  if (isNaN(d.getTime())) return ''
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

export function relTime(s: string | null | undefined, now = Date.now()): string {
  if (!s) return 'never'
  const t = new Date(s).getTime()
  if (isNaN(t) || new Date(s).getFullYear() < 2000) return 'never'
  const diff = Math.round((now - t) / 1000)
  const abs = Math.abs(diff)
  const suffix = diff >= 0 ? 'ago' : 'from now'
  if (abs < 10) return diff >= 0 ? 'just now' : 'in a moment'
  if (abs < 60) return `${abs}s ${suffix}`
  if (abs < 3600) return `${Math.floor(abs / 60)}m ${suffix}`
  if (abs < 86400) return `${Math.floor(abs / 3600)}h ${suffix}`
  return `${Math.floor(abs / 86400)}d ${suffix}`
}

/** "mm:ss" or "h:mm:ss" until the given time; "expired" when past. */
export function countdown(s: string, now = Date.now()): string {
  const left = Math.floor((new Date(s).getTime() - now) / 1000)
  if (isNaN(left)) return ''
  if (left <= 0) return 'expired'
  const h = Math.floor(left / 3600)
  const m = Math.floor((left % 3600) / 60)
  const sec = left % 60
  const pad = (n: number) => String(n).padStart(2, '0')
  return h > 0 ? `${h}:${pad(m)}:${pad(sec)}` : `${m}:${pad(sec)}`
}

export function usd(n: number | null | undefined, digits = 2): string {
  const v = n ?? 0
  if (v > 0 && v < 0.01) return '$' + v.toFixed(4)
  return '$' + v.toLocaleString(undefined, { minimumFractionDigits: digits, maximumFractionDigits: digits })
}

export function num(n: number | null | undefined): string {
  return (n ?? 0).toLocaleString()
}

export function compact(n: number | null | undefined): string {
  const v = n ?? 0
  if (v >= 1_000_000) return (v / 1_000_000).toFixed(1) + 'M'
  if (v >= 10_000) return Math.round(v / 1000) + 'k'
  return v.toLocaleString()
}

export function autonomyLabel(a: Autonomy | number | null | undefined): string {
  return `L${a ?? 0}`
}

export function prettyJSON(v: unknown): string {
  if (v === undefined || v === null) return ''
  if (typeof v === 'string') {
    try {
      return JSON.stringify(JSON.parse(v), null, 2)
    } catch {
      return v
    }
  }
  try {
    return JSON.stringify(v, null, 2)
  } catch {
    return String(v)
  }
}

export function splitList(s: string, sep: RegExp = /[,\n]/): string[] {
  return s
    .split(sep)
    .map((x) => x.trim())
    .filter(Boolean)
}

export function shortId(id: string | null | undefined): string {
  if (!id) return '—'
  return id.length > 14 ? id.slice(0, 12) + '…' : id
}

export function duration(ms: number | null | undefined): string {
  const v = ms ?? 0
  if (v < 1000) return `${v} ms`
  if (v < 60000) return `${(v / 1000).toFixed(1)} s`
  return `${Math.floor(v / 60000)}m ${Math.round((v % 60000) / 1000)}s`
}

export function durationSec(sec: number | null | undefined): string {
  const v = sec ?? 0
  if (!v) return '—'
  if (v < 60) return `${v}s`
  if (v < 3600) return `${Math.round(v / 60)}m`
  return `${(v / 3600).toFixed(1)}h`
}

/** An http(s) URL safe to put in an href (forge links come from the server but are still data). */
export function safeUrl(url: string | null | undefined): string | undefined {
  if (!url) return undefined
  try {
    const u = new URL(url)
    return u.protocol === 'http:' || u.protocol === 'https:' ? u.toString() : undefined
  } catch {
    return undefined
  }
}

/** A same-origin path or an http(s) URL, for full-page navigations the server hands us (SSO login). */
export function safeNavUrl(url: string | null | undefined): string | undefined {
  if (!url) return undefined
  if (url.startsWith('/') && !url.startsWith('//') && !url.startsWith('/\\')) return url
  return safeUrl(url)
}

/** 32 random bytes as hex, for webhook secrets. */
export function randomHex(bytes = 32): string {
  const a = new Uint8Array(bytes)
  crypto.getRandomValues(a)
  return Array.from(a, (b) => b.toString(16).padStart(2, '0')).join('')
}
