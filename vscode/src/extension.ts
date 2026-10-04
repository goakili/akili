// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

import * as vscode from 'vscode'
import { Approvals } from './approvals'
import { Auth } from './auth'
import { ProjectLink } from './project'
import { Sidebar } from './sidebar'
import { Tasks } from './tasks'

export async function activate(ctx: vscode.ExtensionContext): Promise<void> {
  const auth = new Auth(ctx)
  const link = new ProjectLink(ctx, auth)
  const tasks = new Tasks(auth, link)
  const sidebar = new Sidebar(ctx, auth, link, tasks)
  const approvals = new Approvals(auth, (id) => {
    void vscode.commands.executeCommand('akili.sidebar.focus')
    sidebar.openSession(id)
  })

  const status = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 50)
  status.command = 'akili.sidebar.focus'
  const render = () => {
    if (!auth.client) {
      status.text = '$(circle-slash) Akili'
      status.tooltip = 'Akili: not signed in'
      status.command = 'akili.signIn'
    } else {
      const project = link.project ? ` · ${link.project.name}` : ''
      const waiting = approvals.pending ? ` · $(bell-dot) ${approvals.pending}` : ''
      status.text = `$(hubot) Akili${project}${waiting}`
      status.tooltip = `${auth.user?.email} on ${auth.client.baseUrl}${link.project ? `\nProject: ${link.project.name}` : '\nNo project linked'}`
      status.command = 'akili.sidebar.focus'
    }
    status.show()
  }

  ctx.subscriptions.push(
    auth,
    link,
    approvals,
    status,
    vscode.window.registerUriHandler(auth),
    vscode.window.registerWebviewViewProvider(Sidebar.viewId, sidebar, { webviewOptions: { retainContextWhenHidden: true } }),
    vscode.commands.registerCommand('akili.signIn', () => auth.signIn()),
    vscode.commands.registerCommand('akili.signInWithKey', () => auth.signInWithKey()),
    vscode.commands.registerCommand('akili.signOut', () => auth.signOut()),
    vscode.commands.registerCommand('akili.linkProject', () => link.pick()),
    vscode.commands.registerCommand('akili.unlinkProject', () => link.set(null)),
    vscode.commands.registerCommand('akili.openWeb', () => {
      if (auth.client) void vscode.env.openExternal(vscode.Uri.parse(auth.client.baseUrl + (link.project ? `/projects/${link.project.id}` : '/')))
    }),
    auth.onDidChange(() => {
      void vscode.commands.executeCommand('setContext', 'akili.signedIn', !!auth.client)
      sidebar.reload()
      approvals.start()
      void link.refresh()
      render()
    }),
    link.onDidChange(() => {
      sidebar.showProject()
      render()
    }),
    approvals.onDidChangePending(render),
    vscode.workspace.onDidChangeConfiguration((e) => {
      if (e.affectsConfiguration('akili.url')) void auth.restore()
    }),
  )

  render()
  await auth.restore()
}

export function deactivate(): void {}
