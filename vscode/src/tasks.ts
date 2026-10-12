// Copyright (c) 2026 Jonas Kaninda.
//
// This file is part of Akili.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.
//
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
