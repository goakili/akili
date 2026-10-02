// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api, type BusEvent } from '../api'
import { LiveStream, type StreamStatus } from '../lib/sse'

type Listener = (ev: BusEvent) => void

/**
 * The organization event feed (/events/stream): the ONE live connection a tab holds. Browsers allow
 * ~6 HTTP/1.1 connections per host and each SSE stream pins one, so per-view streams starved ordinary
 * requests once a few tabs were open. The session a tab is showing is named with focus(); only that
 * session's token deltas are added to the feed. It also keeps the shell's global facts: pending
 * approvals and the kill switch.
 */
export const useLive = defineStore('live', () => {
  const status = ref<StreamStatus>('closed')
  const pendingApprovals = ref(0)
  const killSwitch = ref(false)
  const version = ref('')
  const listeners = new Set<Listener>()
  const reconnectListeners = new Set<() => void>()
  let stream: LiveStream | null = null
  let focused: string | null = null
  // A focus switch replaces the stream; events in the gap are recovered by resyncing once it opens.
  let resyncOnOpen = false
  let authCheck: () => void = () => {}
  let countTimer: ReturnType<typeof setTimeout> | null = null

  async function refreshCounts() {
    try {
      const o = await api.overview({ quiet: true })
      pendingApprovals.value = o.pending_approvals
      killSwitch.value = o.kill_switch
      version.value = o.version
    } catch {
      /* the shell keeps the last known values */
    }
  }

  function scheduleCounts() {
    if (countTimer) clearTimeout(countTimer)
    countTimer = setTimeout(refreshCounts, 400)
  }

  function start(onAuthCheck: () => void) {
    if (stream) return
    authCheck = onAuthCheck
    refreshCounts()
    connect()
  }

  function connect() {
    stream = new LiveStream(api.eventsStreamURL(focused), {
      onStatus: (s) => {
        status.value = s
        if (s === 'open' && resyncOnOpen) {
          resyncOnOpen = false
          refreshCounts()
          reconnectListeners.forEach((fn) => fn())
        }
      },
      onRepeatedFailure: () => authCheck(),
      onReconnect: () => {
        refreshCounts()
        reconnectListeners.forEach((fn) => fn())
      },
      onEvent: (ev) => {
        switch (ev.type) {
          case 'approval.created':
            pendingApprovals.value++
            scheduleCounts()
            break
          case 'approval.resolved':
            scheduleCounts()
            break
          case 'system.kill_switch':
            killSwitch.value = !!(ev.data as { enabled?: boolean } | undefined)?.enabled
            break
        }
        listeners.forEach((fn) => {
          try {
            fn(ev)
          } catch (e) {
            console.error('event listener failed', e)
          }
        })
      },
    })
  }

  function stop() {
    stream?.close()
    stream = null
  }

  /**
   * Show token deltas for one session (the one on screen), or none. Reconnects the feed; views
   * resync through onReconnect-style reloads, so nothing is lost across the switch.
   */
  function focus(sessionId: string | null) {
    if (focused === sessionId) return
    focused = sessionId
    if (!stream) return
    stream.close()
    resyncOnOpen = true
    connect()
  }

  /** Clear the focus only if it is still this session (the next view may already have taken it). */
  function unfocus(sessionId: string) {
    if (focused === sessionId) focus(null)
  }

  /** Subscribe to org events; returns an unsubscribe function. */
  function on(fn: Listener): () => void {
    listeners.add(fn)
    return () => listeners.delete(fn)
  }

  function onReconnect(fn: () => void): () => void {
    reconnectListeners.add(fn)
    return () => reconnectListeners.delete(fn)
  }

  return { status, pendingApprovals, killSwitch, version, start, stop, focus, unfocus, on, onReconnect, refreshCounts }
})
