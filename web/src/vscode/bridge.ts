// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Inside a VS Code webview, the API key lives in the extension and the control plane would refuse the
// webview's origin anyway. So fetch and EventSource are replaced by versions that ask the extension
// to make the call; the extension only forwards /api/v1 paths it allows (vscode/src/proxy.ts).

interface VSCodeApi {
  postMessage(msg: unknown): void
}
declare function acquireVsCodeApi(): VSCodeApi

export const vscode = acquireVsCodeApi()

type FetchReply = { type: 'fetch:res'; id: number; status: number; contentType: string; body: string; error?: string }
type SSEMessage = { type: 'sse:event' | 'sse:error'; id: number; data?: string }

let seq = 0
const pendingFetches = new Map<number, (r: FetchReply) => void>()
const streams = new Map<number, BridgeEventSource>()
const hostListeners = new Set<(msg: { type: string; [k: string]: unknown }) => void>()

window.addEventListener('message', (e: MessageEvent) => {
  const msg = e.data as { type?: string; id?: number }
  if (!msg || typeof msg.type !== 'string') return
  if (msg.type === 'fetch:res' && typeof msg.id === 'number') {
    pendingFetches.get(msg.id)?.(msg as FetchReply)
    pendingFetches.delete(msg.id)
  } else if ((msg.type === 'sse:event' || msg.type === 'sse:error') && typeof msg.id === 'number') {
    streams.get(msg.id)?.deliver(msg as SSEMessage)
  } else {
    hostListeners.forEach((fn) => fn(msg as { type: string }))
  }
})

/** Messages from the extension that are not fetch or stream traffic (init, project changes). */
export function onHost(fn: (msg: { type: string; [k: string]: unknown }) => void): () => void {
  hostListeners.add(fn)
  return () => hostListeners.delete(fn)
}

function pathOf(input: RequestInfo | URL): string {
  const raw = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
  const u = new URL(raw, 'https://akili.invalid')
  return u.pathname + u.search
}

async function bodyOf(body: BodyInit | null | undefined): Promise<{ body?: string; base64?: boolean }> {
  if (body == null) return {}
  if (typeof body === 'string') return { body }
  if (body instanceof Blob) {
    const bytes = new Uint8Array(await body.arrayBuffer())
    let bin = ''
    for (const b of bytes) bin += String.fromCharCode(b)
    return { body: btoa(bin), base64: true }
  }
  throw new TypeError('unsupported request body')
}

window.fetch = async (input: RequestInfo | URL, init: RequestInit = {}): Promise<Response> => {
  const id = ++seq
  const headers = new Headers(init.headers)
  const reply = new Promise<FetchReply>((resolve) => pendingFetches.set(id, resolve))
  vscode.postMessage({
    type: 'fetch',
    id,
    method: (init.method ?? 'GET').toUpperCase(),
    path: pathOf(input),
    accept: headers.get('Accept') ?? '',
    contentType: headers.get('Content-Type') ?? '',
    ...(await bodyOf(init.body)),
  })
  init.signal?.addEventListener('abort', () => {
    pendingFetches.delete(id)
    vscode.postMessage({ type: 'fetch:abort', id })
  })
  const r = await reply
  if (r.error) throw new TypeError(r.error)
  return new Response(r.body, { status: r.status, headers: r.contentType ? { 'Content-Type': r.contentType } : {} })
}

/** The part of EventSource that lib/sse.ts uses. */
class BridgeEventSource {
  static readonly CONNECTING = 0
  static readonly OPEN = 1
  static readonly CLOSED = 2
  readonly CONNECTING = 0
  readonly OPEN = 1
  readonly CLOSED = 2
  readyState = 0
  readonly url: string
  readonly withCredentials = false
  onopen: ((e: Event) => void) | null = null
  onmessage: ((e: MessageEvent<string>) => void) | null = null
  onerror: ((e: Event) => void) | null = null
  private readonly id = ++seq

  constructor(url: string | URL) {
    this.url = String(url)
    streams.set(this.id, this)
    vscode.postMessage({ type: 'sse:open', id: this.id, path: pathOf(url) })
  }

  deliver(msg: SSEMessage): void {
    if (this.readyState === 2) return
    if (msg.type === 'sse:error') {
      this.readyState = 2
      streams.delete(this.id)
      this.onerror?.(new Event('error'))
      return
    }
    if (this.readyState === 0) {
      this.readyState = 1
      this.onopen?.(new Event('open'))
    }
    this.onmessage?.(new MessageEvent('message', { data: msg.data ?? '' }))
  }

  close(): void {
    if (this.readyState === 2) return
    this.readyState = 2
    streams.delete(this.id)
    vscode.postMessage({ type: 'sse:close', id: this.id })
  }

  addEventListener(): void {}
  removeEventListener(): void {}
  dispatchEvent(): boolean {
    return false
  }
}

;(window as unknown as { EventSource: unknown }).EventSource = BridgeEventSource
