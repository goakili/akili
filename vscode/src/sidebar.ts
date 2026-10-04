// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

import { randomBytes } from 'node:crypto'
import * as vscode from 'vscode'
import type { Auth } from './auth'
import type { ProjectLink } from './project'
import { allowedCall, allowedStream } from './proxy'
import type { Tasks } from './tasks'

type Msg = { type: string; [k: string]: unknown }

/** The Akili sidebar: the web UI's chat and tasks in a webview, its API calls proxied here. */
export class Sidebar implements vscode.WebviewViewProvider {
  static readonly viewId = 'akili.sidebar'
  private view: vscode.WebviewView | null = null
  private readonly fetches = new Map<number, AbortController>()
  private readonly streams = new Map<number, AbortController>()

  constructor(
    private readonly ctx: vscode.ExtensionContext,
    private readonly auth: Auth,
    private readonly link: ProjectLink,
    private readonly tasks: Tasks,
  ) {}

  resolveWebviewView(view: vscode.WebviewView): void {
    this.view = view
    const root = vscode.Uri.joinPath(this.ctx.extensionUri, 'media', 'webview')
    view.webview.options = { enableScripts: true, localResourceRoots: [root] }
    view.webview.html = this.html(view.webview, root)
    view.webview.onDidReceiveMessage((m: Msg) => void this.onMessage(m))
    view.onDidDispose(() => {
      this.closeAll()
      this.view = null
    })
  }

  /** Reloads the webview, e.g. after signing in or out. */
  reload(): void {
    if (!this.view) return
    this.closeAll()
    this.view.webview.html = this.html(this.view.webview, vscode.Uri.joinPath(this.ctx.extensionUri, 'media', 'webview'))
  }

  showProject(): void {
    void this.view?.webview.postMessage({ type: 'project', project: this.link.project })
  }

  openSession(id: string): void {
    void this.view?.webview.postMessage({ type: 'openSession', id })
  }

  private html(webview: vscode.Webview, root: vscode.Uri): string {
    const nonce = randomBytes(16).toString('base64')
    const js = webview.asWebviewUri(vscode.Uri.joinPath(root, 'webview.js'))
    const css = webview.asWebviewUri(vscode.Uri.joinPath(root, 'webview.css'))
    if (!this.auth.client) {
      return `<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'nonce-${nonce}';">
<style nonce="${nonce}">body{padding:12px;font-family:var(--vscode-font-family);color:var(--vscode-foreground)}</style></head>
<body><p>Sign in to Akili to chat with agents and run tasks on this project.</p><p>Run <b>Akili: Sign in</b> from the Command Palette.</p></body></html>`
    }
    // connect-src 'none': every API call goes through the extension, never from the webview itself.
    const csp = [
      "default-src 'none'",
      `img-src ${webview.cspSource} data:`,
      `font-src ${webview.cspSource}`,
      `style-src ${webview.cspSource} 'unsafe-inline'`,
      `script-src 'nonce-${nonce}'`,
      "connect-src 'none'",
    ].join('; ')
    return `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="${csp}">
<meta name="viewport" content="width=device-width, initial-scale=1">
<link rel="stylesheet" href="${css}"></head>
<body><div id="app"></div><script type="module" nonce="${nonce}" src="${js}"></script></body></html>`
  }

  private post(msg: Msg): void {
    void this.view?.webview.postMessage(msg)
  }

  private async onMessage(m: Msg): Promise<void> {
    switch (m.type) {
      case 'ready':
        this.showProject()
        return
      case 'fetch':
        return this.proxyFetch(m)
      case 'fetch:abort':
        this.fetches.get(m.id as number)?.abort()
        return
      case 'sse:open':
        return this.proxyStream(m)
      case 'sse:close':
        this.streams.get(m.id as number)?.abort()
        this.streams.delete(m.id as number)
        return
      case 'openWeb':
        if (typeof m.path === 'string' && m.path.startsWith('/') && !m.path.startsWith('//') && this.auth.client) {
          void vscode.env.openExternal(vscode.Uri.parse(this.auth.client.baseUrl + m.path))
        }
        return
      case 'openUrl':
        if (typeof m.url === 'string' && /^https?:\/\//.test(m.url)) void vscode.env.openExternal(vscode.Uri.parse(m.url))
        return
      case 'openDiff':
        if (typeof m.taskId === 'string') void this.tasks.openDiff(m.taskId)
        return
      case 'checkout':
        if (typeof m.taskId === 'string' && typeof m.branch === 'string') void this.tasks.checkout(m.taskId, m.branch)
        return
      case 'unauthorized':
        void vscode.window.showWarningMessage('Akili: your sign-in is no longer valid.', 'Sign in').then((a) => {
          if (a) void vscode.commands.executeCommand('akili.signIn')
        })
        return
    }
  }

  private async proxyFetch(m: Msg): Promise<void> {
    const id = m.id as number
    const method = String(m.method ?? 'GET')
    const path = String(m.path ?? '')
    const client = this.auth.client
    if (!client || !allowedCall(method, path)) {
      this.post({ type: 'fetch:res', id, status: 403, contentType: 'application/json', body: JSON.stringify({ error: { message: 'not available in VS Code' } }) })
      return
    }
    const ctl = new AbortController()
    this.fetches.set(id, ctl)
    try {
      const body = typeof m.body === 'string' ? { text: m.body, base64: m.base64 === true, contentType: String(m.contentType ?? '') } : undefined
      const r = await client.raw(method, path, body, String(m.accept ?? ''), ctl.signal)
      this.post({ type: 'fetch:res', id, ...r })
    } catch (e) {
      if (!ctl.signal.aborted) this.post({ type: 'fetch:res', id, status: 0, contentType: '', body: '', error: (e as Error).message })
    } finally {
      this.fetches.delete(id)
    }
  }

  private async proxyStream(m: Msg): Promise<void> {
    const id = m.id as number
    const path = String(m.path ?? '')
    const client = this.auth.client
    if (!client || !allowedStream(path)) {
      this.post({ type: 'sse:error', id })
      return
    }
    const ctl = new AbortController()
    this.streams.set(id, ctl)
    try {
      await client.stream(path, (data) => this.post({ type: 'sse:event', id, data }), ctl.signal)
    } catch {
      /* reported below */
    }
    if (this.streams.delete(id)) this.post({ type: 'sse:error', id })
  }

  private closeAll(): void {
    this.fetches.forEach((c) => c.abort())
    this.streams.forEach((c) => c.abort())
    this.fetches.clear()
    this.streams.clear()
  }
}
