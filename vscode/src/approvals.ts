// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

import * as vscode from 'vscode'
import type { Auth } from './auth'
import type { Approval, BusEvent } from './client'

/**
 * Follows the organization's event stream and asks for a decision when an agent in one of the user's
 * own sessions waits for an approval. The decision is made on the control plane, bound to the exact
 * call, like any other approval.
 */
export class Approvals {
  private ctl: AbortController | null = null
  private readonly mySessions = new Set<string>()
  private readonly changed = new vscode.EventEmitter<number>()
  readonly onDidChangePending = this.changed.event
  pending = 0

  constructor(
    private readonly auth: Auth,
    private readonly open: (sessionId: string) => void,
  ) {}

  start(): void {
    this.stop()
    if (!this.auth.client || !this.auth.user) return
    const ctl = new AbortController()
    this.ctl = ctl
    void this.loop(ctl)
  }

  stop(): void {
    this.ctl?.abort()
    this.ctl = null
    this.mySessions.clear()
  }

  private async loop(ctl: AbortController): Promise<void> {
    let delay = 1000
    while (!ctl.signal.aborted && this.auth.client) {
      try {
        await this.refresh()
        await this.auth.client.stream('/api/v1/events/stream', (d) => this.onEvent(d), ctl.signal)
        delay = 1000
      } catch {
        delay = Math.min(delay * 2, 30_000)
      }
      if (!ctl.signal.aborted) await new Promise((r) => setTimeout(r, delay))
    }
  }

  private async refresh(): Promise<void> {
    const client = this.auth.client
    if (!client) return
    const list = (await client.json<Approval[] | null>('GET', '/approvals?status=pending')) ?? []
    this.pending = list.length
    this.changed.fire(this.pending)
  }

  private onEvent(data: string): void {
    let ev: BusEvent
    try {
      ev = JSON.parse(data) as BusEvent
    } catch {
      return
    }
    if (ev.type === 'approval.resolved') void this.refresh()
    if (ev.type === 'approval.created' && ev.session_id) void this.ask(ev.session_id, ev.data as Approval | undefined)
  }

  private async mine(sessionId: string): Promise<boolean> {
    if (this.mySessions.has(sessionId)) return true
    const client = this.auth.client
    if (!client || !this.auth.user) return false
    try {
      const d = await client.json<{ session: { created_by: string } }>('GET', `/sessions/${encodeURIComponent(sessionId)}`)
      if (d.session.created_by !== this.auth.user.id) return false
      this.mySessions.add(sessionId)
      return true
    } catch {
      return false
    }
  }

  private async ask(sessionId: string, approval: Approval | undefined): Promise<void> {
    await this.refresh()
    if (!approval?.id || !(await this.mine(sessionId))) return
    const args = JSON.stringify(approval.input ?? {})
    const pick = await vscode.window.showWarningMessage(
      `Akili: an agent asks to run ${approval.tool} (${approval.risk} risk).`,
      { modal: false, detail: args.length > 300 ? args.slice(0, 300) + '…' : args },
      'Open',
      'Approve',
      'Deny',
    )
    const client = this.auth.client
    if (!pick || !client) return
    if (pick === 'Open') {
      this.open(sessionId)
      return
    }
    // Approve only after showing the full call: the notification may have cut the arguments.
    if (pick === 'Approve') {
      const sure = await vscode.window.showWarningMessage(`Approve ${approval.tool}?`, { modal: true, detail: JSON.stringify(approval.input ?? {}, null, 2) }, 'Approve')
      if (sure !== 'Approve') return
    }
    try {
      await client.json('POST', `/approvals/${encodeURIComponent(approval.id)}/${pick === 'Approve' ? 'approve' : 'deny'}`, { note: 'from VS Code' })
    } catch (e) {
      void vscode.window.showErrorMessage(`Akili: ${(e as Error).message}`)
    }
  }

  dispose(): void {
    this.stop()
    this.changed.dispose()
  }
}
