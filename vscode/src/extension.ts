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
  const focus = () => vscode.commands.executeCommand('akili.sidebar.focus')
  const approvals = new Approvals(auth, (id) => {
    void focus()
    sidebar.openSession(id)
  })

  const status = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 50)
  status.command = 'akili.showMenu'
  const render = () => {
    if (auth.state === 'signingIn') {
      status.text = '$(sync~spin) Akili'
      status.tooltip = 'Akili: waiting for the browser sign-in'
    } else if (!auth.client) {
      status.text = '$(account) Akili: Sign in'
      status.tooltip = auth.problem || 'Akili: not signed in'
    } else {
      const project = link.project ? ` · ${link.project.name}` : ''
      const waiting = approvals.pending ? ` $(bell-dot) ${approvals.pending}` : ''
      status.text = `$(hubot) Akili${project}${waiting}`
      const md = new vscode.MarkdownString(
        `**Akili** · ${auth.user?.email}\n\n${auth.client.baseUrl}\n\n${link.project ? `Project: **${link.project.name}**` : 'No project linked'}` +
          (approvals.pending ? `\n\n$(bell-dot) ${approvals.pending} approval(s) waiting` : ''),
        true,
      )
      status.tooltip = md
    }
    status.backgroundColor = approvals.pending && auth.client ? new vscode.ThemeColor('statusBarItem.warningBackground') : undefined
    status.show()
  }

  // A menu that gathers every action, so nothing is only reachable from the Command Palette.
  const showMenu = async () => {
    type Item = vscode.QuickPickItem & { run?: () => unknown }
    const items: Item[] = []
    if (!auth.client) {
      items.push(
        { label: '$(sign-in) Sign in with the browser', run: () => auth.signIn() },
        { label: '$(key) Sign in with an API key', run: () => auth.signInWithKey() },
        { label: '$(server) Change server', description: auth.server() ?? 'not set', run: () => auth.changeServer() },
      )
    } else {
      items.push(
        { label: '$(hubot) Open Akili', run: focus },
        { label: '$(comment-discussion) New chat', run: () => vscode.commands.executeCommand('akili.newChat') },
        { label: '$(tasklist) New task', run: () => vscode.commands.executeCommand('akili.newTask') },
        { label: '', kind: vscode.QuickPickItemKind.Separator },
        link.project
          ? { label: '$(link) Change linked project', description: link.project.name, run: () => link.pick() }
          : { label: '$(link) Link this folder to a project', run: () => link.pick() },
        { label: '$(link-external) Open in the browser', run: () => vscode.commands.executeCommand('akili.openWeb') },
        { label: '$(gear) Settings', run: () => vscode.commands.executeCommand('akili.openSettings') },
        { label: '', kind: vscode.QuickPickItemKind.Separator },
        { label: '$(server) Change server', description: auth.client.baseUrl, run: () => auth.changeServer() },
        { label: '$(sign-out) Sign out', description: auth.user?.email, run: () => auth.signOut() },
      )
    }
    const pick = await vscode.window.showQuickPick(items, { title: auth.user ? `Akili · ${auth.user.email}` : 'Akili' })
    await pick?.run?.()
  }

  const contexts = () => {
    void vscode.commands.executeCommand('setContext', 'akili.signedIn', !!auth.client)
    void vscode.commands.executeCommand('setContext', 'akili.projectLinked', !!link.project)
  }

  // Title-bar actions need a linked project; open the sidebar first so the webview can receive them.
  const sidebarAction = (type: 'newChat' | 'newTask' | 'refresh') => async () => {
    if (!link.project) {
      await link.pick()
      if (!link.project) return
    }
    await focus()
    sidebar.action(type)
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
    vscode.commands.registerCommand('akili.cancelSignIn', () => auth.cancelSignIn()),
    vscode.commands.registerCommand('akili.changeServer', () => auth.changeServer()),
    vscode.commands.registerCommand('akili.signOut', () => auth.signOut()),
    vscode.commands.registerCommand('akili.linkProject', () => link.pick()),
    vscode.commands.registerCommand('akili.unlinkProject', () => link.set(null)),
    vscode.commands.registerCommand('akili.newChat', sidebarAction('newChat')),
    vscode.commands.registerCommand('akili.newTask', sidebarAction('newTask')),
    vscode.commands.registerCommand('akili.refresh', async () => {
      await link.refresh()
      sidebar.action('refresh')
    }),
    vscode.commands.registerCommand('akili.showMenu', showMenu),
    vscode.commands.registerCommand('akili.openSettings', () => vscode.commands.executeCommand('workbench.action.openSettings', '@ext:goakili.akili')),
    vscode.commands.registerCommand('akili.openWeb', () => {
      if (auth.client) void vscode.env.openExternal(vscode.Uri.parse(auth.client.baseUrl + (link.project ? `/projects/${link.project.id}` : '/')))
    }),
    auth.onDidChange(() => {
      contexts()
      sidebar.reload()
      if (auth.client) {
        approvals.start()
        void link.refresh()
      } else {
        approvals.stop()
        void link.clear()
      }
      render()
    }),
    link.onDidChange(() => {
      contexts()
      sidebar.showProject()
      render()
    }),
    approvals.onDidChangePending(render),
    vscode.workspace.onDidChangeConfiguration((e) => {
      if (e.affectsConfiguration('akili.url')) void auth.restore()
    }),
  )

  contexts()
  render()
  await auth.restore()
}

export function deactivate(): void {}
