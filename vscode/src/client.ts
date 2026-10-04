// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

import { SSEParser } from './sse'

export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message)
  }
}

export interface RawResponse {
  status: number
  contentType: string
  body: string
}

export interface Project {
  id: string
  name: string
  slug: string
  owner: string
  repo: string
  web_url: string
  agent_id: string | null
}

export interface User {
  id: string
  email: string
  name: string
  role: string
}

export interface BusEvent {
  type: string
  session_id?: string
  task_id?: string
  data?: unknown
}

export interface Approval {
  id: string
  session_id: string
  tool: string
  input: unknown
  risk: string
  reason: string
  status: string
}

/** The control plane API, authenticated with the user's API key. */
export class Client {
  constructor(
    readonly baseUrl: string,
    private readonly key: string,
  ) {}

  /** Makes a call and returns the response as is (the webview proxy parses it itself). */
  async raw(method: string, path: string, body?: { text?: string; base64?: boolean; contentType?: string }, accept = '', signal?: AbortSignal): Promise<RawResponse> {
    const headers: Record<string, string> = { Authorization: `Bearer ${this.key}`, Accept: accept || 'application/json' }
    let payload: string | Buffer | undefined
    if (body?.text !== undefined) {
      headers['Content-Type'] = body.contentType || 'application/json'
      payload = body.base64 ? Buffer.from(body.text, 'base64') : body.text
    }
    const res = await fetch(this.baseUrl + path, { method, headers, body: payload, signal, redirect: 'error' })
    return { status: res.status, contentType: res.headers.get('Content-Type') ?? '', body: await res.text() }
  }

  async json<T>(method: string, path: string, body?: unknown): Promise<T> {
    const r = await this.raw(method, '/api/v1' + path, body === undefined ? undefined : { text: JSON.stringify(body) })
    let env: { data?: T; error?: { message?: string } } | null = null
    try {
      env = r.body ? JSON.parse(r.body) : null
    } catch {
      env = null
    }
    if (r.status < 200 || r.status >= 300) throw new ApiError(r.status, env?.error?.message || `HTTP ${r.status}`)
    return (env?.data ?? (env as T)) as T
  }

  async text(path: string): Promise<string> {
    const r = await this.raw('GET', '/api/v1' + path, undefined, 'text/plain, text/x-diff, application/json')
    if (r.status < 200 || r.status >= 300) {
      let msg = `HTTP ${r.status}`
      try {
        msg = JSON.parse(r.body)?.error?.message || msg
      } catch {
        /* not JSON */
      }
      throw new ApiError(r.status, msg)
    }
    return r.body
  }

  me(): Promise<{ user: User }> {
    return this.json('GET', '/auth/me')
  }

  /**
   * Follows a Server-Sent Events stream until signal aborts or the stream ends. onData gets each
   * event's data; the promise rejects with ApiError when the server refuses the stream.
   */
  async stream(path: string, onData: (data: string) => void, signal: AbortSignal): Promise<void> {
    const res = await fetch(this.baseUrl + path, {
      headers: { Authorization: `Bearer ${this.key}`, Accept: 'text/event-stream' },
      signal,
      redirect: 'error',
    })
    if (!res.ok || !res.body) throw new ApiError(res.status, `stream refused: HTTP ${res.status}`)
    const parser = new SSEParser(onData)
    const decoder = new TextDecoder()
    for await (const chunk of res.body as unknown as AsyncIterable<Uint8Array>) {
      parser.push(decoder.decode(chunk, { stream: true }))
    }
  }
}
