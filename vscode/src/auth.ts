// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

import * as os from 'node:os'
import * as vscode from 'vscode'
import { ApiError, Client, type User } from './client'
import { newSignInRequest, type SignInRequest } from './pkce'
import { normalizeServerUrl } from './workspace'

const EXTENSION_ID = 'goakili.akili'
const SIGN_IN_TIMEOUT_MS = 5 * 60 * 1000

/** Keys are stored per server, so switching akili.url never sends one server's key to another. */
function secretKey(server: string): string {
  return `akili.apiKey:${server}`
}

export class Auth implements vscode.UriHandler {
  private pending: (SignInRequest & { server: string; resolve: (code: string) => void; reject: (e: Error) => void }) | null = null
  private readonly changed = new vscode.EventEmitter<void>()
  readonly onDidChange = this.changed.event
  client: Client | null = null
  user: User | null = null

  constructor(private readonly ctx: vscode.ExtensionContext) {}

  /** The control plane from user settings. Never from workspace settings: a repository must not choose it. */
  server(): string | undefined {
    const raw = vscode.workspace.getConfiguration('akili').inspect<string>('url')?.globalValue ?? ''
    return raw ? normalizeServerUrl(raw) : undefined
  }

  async restore(): Promise<void> {
    const server = this.server()
    const key = server ? await this.ctx.secrets.get(secretKey(server)) : undefined
    await this.use(server, key)
  }

  private async use(server: string | undefined, key: string | undefined): Promise<void> {
    this.client = null
    this.user = null
    if (server && key) {
      const client = new Client(server, key)
      try {
        this.user = (await client.me()).user
        this.client = client
      } catch (e) {
        if (e instanceof ApiError && e.status === 401) await this.ctx.secrets.delete(secretKey(server))
        else void vscode.window.showWarningMessage(`Akili: cannot reach ${server}: ${(e as Error).message}`)
      }
    }
    this.changed.fire()
  }

  private async askServer(): Promise<string | undefined> {
    let server = this.server()
    if (server) return server
    const raw = await vscode.window.showInputBox({
      title: 'Akili control plane',
      prompt: 'The URL of your Akili control plane',
      placeHolder: 'https://akili.example.com',
      ignoreFocusOut: true,
      validateInput: (v) => (normalizeServerUrl(v) ? undefined : 'Use https:// (http:// only for localhost), without a path query or credentials'),
    })
    server = raw ? normalizeServerUrl(raw) : undefined
    if (server) await vscode.workspace.getConfiguration('akili').update('url', server, vscode.ConfigurationTarget.Global)
    return server
  }

  /** Signs in through the browser: Akili approves, then hands a one-time code back to this editor. */
  async signIn(): Promise<void> {
    const server = await this.askServer()
    if (!server) return
    const req = newSignInRequest()
    // VS Code adds windowId to its own callback URI so the answer reaches this window. Akili builds
    // the callback itself from the scheme and that id: passing a whole URL through the browser got
    // re-encoded on the way.
    const callback = await vscode.env.asExternalUri(vscode.Uri.parse(`${vscode.env.uriScheme}://${EXTENSION_ID}/callback`))
    const window = new URLSearchParams(callback.query).get('windowId') ?? ''
    const client = `${vscode.env.appName} on ${os.hostname()}`.slice(0, 80)
    const params: Record<string, string> = { challenge: req.challenge, state: req.state, client, editor: vscode.env.uriScheme, window }
    const url = server + '/vscode/authorize?' + Object.entries(params).map(([k, v]) => `${k}=${encodeURIComponent(v)}`).join('&')

    const code = new Promise<string>((resolve, reject) => {
      this.pending = { ...req, server, resolve, reject }
      setTimeout(() => reject(new Error('the sign-in timed out')), SIGN_IN_TIMEOUT_MS)
    })
    await vscode.env.openExternal(vscode.Uri.parse(url))
    try {
      const got = await vscode.window.withProgress(
        { location: vscode.ProgressLocation.Notification, title: 'Akili: approve the sign-in in your browser…', cancellable: true },
        (_p, token) =>
          new Promise<string>((resolve, reject) => {
            token.onCancellationRequested(() => reject(new Error('cancelled')))
            code.then(resolve, reject)
          }),
      )
      const res = await fetch(server + '/api/v1/auth/vscode/token', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
        body: JSON.stringify({ code: got, verifier: req.verifier }),
        redirect: 'error',
      })
      const body = (await res.json().catch(() => null)) as { data?: { secret?: string }; error?: { message?: string } } | null
      if (!res.ok || !body?.data?.secret) throw new Error(body?.error?.message || `HTTP ${res.status}`)
      await this.ctx.secrets.store(secretKey(server), body.data.secret)
      await this.use(server, body.data.secret)
      if (this.user) void vscode.window.showInformationMessage(`Akili: signed in as ${this.user.email}.`)
    } catch (e) {
      if ((e as Error).message !== 'cancelled') void vscode.window.showErrorMessage(`Akili: sign-in failed: ${(e as Error).message}`)
    } finally {
      this.pending = null
    }
  }

  /** For editors without a working URI handler: paste an API key created in Settings → API keys. */
  async signInWithKey(): Promise<void> {
    const server = await this.askServer()
    if (!server) return
    const key = await vscode.window.showInputBox({ title: 'Akili API key', prompt: 'An API key (ak_…) from Settings → API keys', password: true, ignoreFocusOut: true })
    if (!key) return
    if (!key.startsWith('ak_')) {
      void vscode.window.showErrorMessage('Akili: that is not an Akili API key (ak_…).')
      return
    }
    await this.use(server, key)
    if (this.user) {
      await this.ctx.secrets.store(secretKey(server), key)
      void vscode.window.showInformationMessage(`Akili: signed in as ${this.user.email}.`)
    } else {
      void vscode.window.showErrorMessage('Akili: the key was refused.')
    }
  }

  async signOut(): Promise<void> {
    const server = this.server()
    if (server) await this.ctx.secrets.delete(secretKey(server))
    await this.use(undefined, undefined)
    void vscode.window.showInformationMessage('Akili: signed out. Revoke the key under Settings → API keys if you no longer need it.')
  }

  /** The URI handler: the browser hands back the code with the state it was given. */
  handleUri(uri: vscode.Uri): void {
    if (uri.path !== '/callback' || !this.pending) return
    const q = new URLSearchParams(uri.query)
    if (q.get('state') !== this.pending.state) {
      this.pending.reject(new Error('the sign-in response does not match this sign-in'))
      return
    }
    const code = q.get('code')
    if (code) this.pending.resolve(code)
  }

  dispose(): void {
    this.changed.dispose()
  }
}
