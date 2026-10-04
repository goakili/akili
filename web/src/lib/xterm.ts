// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// xterm.js setup shared by the live terminal and the recording player: one look, both themes.
import { Terminal, type ITheme } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'

const DARK: ITheme = {
  background: '#0d0d1a',
  foreground: '#e5e7eb',
  cursor: '#51b395',
  cursorAccent: '#0d0d1a',
  selectionBackground: 'rgba(81, 179, 149, 0.32)',
  black: '#1e1e36',
  red: '#f87171',
  green: '#4ade80',
  yellow: '#fbbf24',
  blue: '#60a5fa',
  magenta: '#c084fc',
  cyan: '#22d3ee',
  white: '#d1d5db',
  brightBlack: '#6b7280',
  brightRed: '#fca5a5',
  brightGreen: '#86efac',
  brightYellow: '#fde68a',
  brightBlue: '#93c5fd',
  brightMagenta: '#d8b4fe',
  brightCyan: '#67e8f9',
  brightWhite: '#ffffff',
}

// On white, the default ANSI palette is unreadable (yellow, white); darken every colour.
const LIGHT: ITheme = {
  background: '#ffffff',
  foreground: '#111827',
  cursor: '#1b6152',
  cursorAccent: '#ffffff',
  selectionBackground: 'rgba(32, 122, 99, 0.22)',
  black: '#111827',
  red: '#b91c1c',
  green: '#15803d',
  yellow: '#a16207',
  blue: '#1d4ed8',
  magenta: '#7e22ce',
  cyan: '#0e7490',
  white: '#6b7280',
  brightBlack: '#4b5563',
  brightRed: '#dc2626',
  brightGreen: '#16a34a',
  brightYellow: '#ca8a04',
  brightBlue: '#2563eb',
  brightMagenta: '#9333ea',
  brightCyan: '#0891b2',
  brightWhite: '#374151',
}

export function xtermTheme(mode: 'light' | 'dark'): ITheme {
  return mode === 'dark' ? DARK : LIGHT
}

export function createTerminal(mode: 'light' | 'dark', opts: { readonly?: boolean; cols?: number; rows?: number } = {}) {
  const term = new Terminal({
    theme: xtermTheme(mode),
    fontFamily: "'JetBrains Mono', ui-monospace, SFMono-Regular, Menlo, monospace",
    fontSize: 13,
    lineHeight: 1.15,
    cursorBlink: !opts.readonly,
    disableStdin: !!opts.readonly,
    scrollback: 5000,
    ...(opts.cols ? { cols: opts.cols } : {}),
    ...(opts.rows ? { rows: opts.rows } : {}),
  })
  const fit = new FitAddon()
  term.loadAddon(fit)
  return { term, fit }
}

/** Calls fn at most once per `ms` after the last call. */
export function debounce<A extends unknown[]>(fn: (...a: A) => void, ms: number) {
  let t: ReturnType<typeof setTimeout> | null = null
  const run = (...a: A) => {
    if (t) clearTimeout(t)
    t = setTimeout(() => fn(...a), ms)
  }
  run.cancel = () => t && clearTimeout(t)
  return run
}
