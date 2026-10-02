// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

import type { BusEvent } from '../api'

export type StreamStatus = 'connecting' | 'open' | 'reconnecting' | 'closed'

export interface LiveStreamOptions {
  onEvent: (ev: BusEvent) => void
  /** Called after the stream comes back from a drop (not on the first connect). */
  onReconnect?: () => void
  onStatus?: (s: StreamStatus) => void
  /** Called after a few consecutive failures, e.g. to check whether the session expired. */
  onRepeatedFailure?: () => void
}

const MIN_DELAY = 1000
const MAX_DELAY = 30000

// Streams must not outlive the page: a document kept in the back/forward cache would otherwise hold
// its SSE sockets open, and browsers allow only ~6 HTTP/1.1 connections per host, so a few
// navigations would starve every later request. Suspend on pagehide, resume (and resync) on pageshow.
const active = new Set<LiveStream>()
if (typeof window !== 'undefined') {
  window.addEventListener('pagehide', () => active.forEach((s) => s.suspend()))
  window.addEventListener('pageshow', (e) => {
    if (e.persisted) active.forEach((s) => s.resume())
  })
}

/**
 * A reconnecting EventSource. The server sends unnamed `data:` messages, one JSON bus event each,
 * plus "ready" on connect and "ping" keep-alives.
 */
export class LiveStream {
  private es: EventSource | null = null
  private timer: ReturnType<typeof setTimeout> | null = null
  private delay = MIN_DELAY
  private failures = 0
  private everOpened = false
  private stopped = false

  constructor(
    private readonly url: string,
    private readonly opts: LiveStreamOptions,
  ) {
    active.add(this)
    this.connect()
  }

  /** Drops the connection without giving up; resume() reconnects and reports a reconnect. */
  suspend(): void {
    if (this.timer) clearTimeout(this.timer)
    this.timer = null
    this.es?.close()
    this.es = null
  }

  resume(): void {
    if (this.stopped || this.es) return
    this.everOpened = true
    this.delay = MIN_DELAY
    this.connect()
  }

  private connect(): void {
    if (this.stopped) return
    this.opts.onStatus?.(this.everOpened ? 'reconnecting' : 'connecting')
    const es = new EventSource(this.url, { withCredentials: true })
    this.es = es
    es.onmessage = (msg: MessageEvent<string>) => {
      let ev: BusEvent
      try {
        ev = JSON.parse(msg.data) as BusEvent
      } catch {
        return
      }
      if (ev.type === 'ready') {
        const wasReconnect = this.everOpened
        this.everOpened = true
        this.delay = MIN_DELAY
        this.failures = 0
        this.opts.onStatus?.('open')
        if (wasReconnect) this.opts.onReconnect?.()
        return
      }
      if (ev.type === 'ping') return
      this.opts.onEvent(ev)
    }
    es.onerror = () => {
      // Take reconnection into our own hands so we control the backoff and can resync.
      es.close()
      if (this.es === es) this.es = null
      if (this.stopped) return
      this.failures++
      if (this.failures === 3) this.opts.onRepeatedFailure?.()
      this.opts.onStatus?.('reconnecting')
      const jitter = Math.random() * 0.3 * this.delay
      this.timer = setTimeout(() => this.connect(), this.delay + jitter)
      this.delay = Math.min(this.delay * 2, MAX_DELAY)
    }
  }

  close(): void {
    active.delete(this)
    this.stopped = true
    if (this.timer) clearTimeout(this.timer)
    this.es?.close()
    this.es = null
    this.opts.onStatus?.('closed')
  }
}
