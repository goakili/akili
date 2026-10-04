// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

import * as vscode from 'vscode'
import type { Auth } from './auth'
import { git, type ProjectLink } from './project'
import { validTaskBranch } from './workspace'

/** Reviewing a task's work locally: its diff, or its branch checked out. */
export class Tasks {
  constructor(
    private readonly auth: Auth,
    private readonly link: ProjectLink,
  ) {}

  async openDiff(taskId: string): Promise<void> {
    const client = this.auth.client
    if (!client) return
    try {
      const diff = await client.text(`/tasks/${encodeURIComponent(taskId)}/diff`)
      const doc = await vscode.workspace.openTextDocument({ language: 'diff', content: diff || '(no changes)' })
      await vscode.window.showTextDocument(doc, { preview: true })
    } catch (e) {
      void vscode.window.showErrorMessage(`Akili: cannot load the diff: ${(e as Error).message}`)
    }
  }

  /** Fetches akili/<task> from origin and switches to it, refusing to touch uncommitted work. */
  async checkout(taskId: string, branch: string): Promise<void> {
    const folder = this.link.folder()
    if (!folder || !validTaskBranch(branch)) {
      void vscode.window.showErrorMessage('Akili: this task has no branch to check out.')
      return
    }
    if (!vscode.workspace.isTrusted) return
    const cwd = folder.uri.fsPath
    try {
      if (await git(cwd, 'status', '--porcelain')) {
        void vscode.window.showWarningMessage('Akili: commit or stash your changes before checking out a task branch.')
        return
      }
      await vscode.window.withProgress({ location: vscode.ProgressLocation.Notification, title: `Akili: fetching ${branch}…` }, () =>
        git(cwd, 'fetch', 'origin', `+refs/heads/${branch}:refs/remotes/origin/${branch}`),
      )
      const exists = await git(cwd, 'branch', '--list', branch)
      if (exists) await git(cwd, 'switch', branch)
      else await git(cwd, 'switch', '--track', '-c', branch, `origin/${branch}`)
      void vscode.window.showInformationMessage(`Akili: on ${branch} (task ${taskId}).`)
    } catch (e) {
      void vscode.window.showErrorMessage(`Akili: checkout failed: ${(e as Error).message}`)
    }
  }
}
