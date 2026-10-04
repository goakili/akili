// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

import { execFile } from 'node:child_process'
import { promisify } from 'node:util'
import * as vscode from 'vscode'
import type { Auth } from './auth'
import { ApiError, type Project } from './client'
import { projectSlugFrom } from './workspace'

const run = promisify(execFile)
const STATE_KEY = 'akili.projectId'

export async function git(cwd: string, ...args: string[]): Promise<string> {
  const { stdout } = await run('git', args, { cwd, timeout: 60_000 })
  return stdout.trim()
}

/** Links the open folder to an Akili project. The link is kept in workspace state, per folder. */
export class ProjectLink {
  project: Project | null = null
  private suggested = false
  private readonly changed = new vscode.EventEmitter<void>()
  readonly onDidChange = this.changed.event

  constructor(
    private readonly ctx: vscode.ExtensionContext,
    private readonly auth: Auth,
  ) {}

  folder(): vscode.WorkspaceFolder | undefined {
    return vscode.workspace.workspaceFolders?.[0]
  }

  async refresh(): Promise<void> {
    this.project = null
    const client = this.auth.client
    const id = this.ctx.workspaceState.get<string>(STATE_KEY)
    if (client && id) {
      try {
        this.project = await client.json<Project>('GET', `/projects/${encodeURIComponent(id)}`)
      } catch (e) {
        if (e instanceof ApiError && e.status === 404) await this.ctx.workspaceState.update(STATE_KEY, undefined)
      }
    }
    this.changed.fire()
    if (client && !this.project && vscode.workspace.isTrusted && !this.suggested) {
      this.suggested = true
      await this.suggest()
    }
  }

  /** Forgets the loaded project (signed out); the folder's link is kept for the next sign-in. */
  clear(): void {
    this.project = null
    this.suggested = false
    this.changed.fire()
  }

  /** Finds the project by the folder's git remote, or by the slug in .akili.json, and offers to link it. */
  private async suggest(): Promise<void> {
    const client = this.auth.client
    const folder = this.folder()
    const mode = vscode.workspace.getConfiguration('akili').get<string>('linkProjects', 'ask')
    if (!client || !folder || mode === 'never') return
    let found: Project | undefined
    try {
      const remote = await git(folder.uri.fsPath, 'remote', 'get-url', 'origin')
      found = await client.json<Project>('GET', `/projects/resolve?remote=${encodeURIComponent(remote)}`)
    } catch {
      found = await this.fromFile(folder)
    }
    if (!found) return
    if (mode === 'auto') {
      await this.set(found)
      return
    }
    const pick = await vscode.window.showInformationMessage(`Link this folder to the Akili project "${found.name}"?`, 'Link', 'Not now', 'Never ask')
    if (pick === 'Link') await this.set(found)
    if (pick === 'Never ask') await vscode.workspace.getConfiguration('akili').update('linkProjects', 'never', vscode.ConfigurationTarget.Global)
  }

  private async fromFile(folder: vscode.WorkspaceFolder): Promise<Project | undefined> {
    try {
      const bytes = await vscode.workspace.fs.readFile(vscode.Uri.joinPath(folder.uri, '.akili.json'))
      const slug = projectSlugFrom(new TextDecoder().decode(bytes))
      if (!slug || !this.auth.client) return undefined
      const all = await this.auth.client.json<Project[] | null>('GET', '/projects')
      return (all ?? []).find((p) => p.slug === slug)
    } catch {
      return undefined
    }
  }

  async pick(): Promise<void> {
    const client = this.auth.client
    if (!client) {
      void vscode.window.showWarningMessage('Akili: sign in first.')
      return
    }
    const all = (await client.json<Project[] | null>('GET', '/projects')) ?? []
    if (!all.length) {
      void vscode.window.showInformationMessage('Akili: there are no projects yet. An admin adds them under Projects.')
      return
    }
    const choice = await vscode.window.showQuickPick(
      all.map((p) => ({ label: p.name, description: `${p.owner}/${p.repo}`, project: p })),
      { title: 'Link this folder to an Akili project' },
    )
    if (choice) await this.set(choice.project)
  }

  async set(project: Project | null): Promise<void> {
    await this.ctx.workspaceState.update(STATE_KEY, project?.id)
    this.project = project
    this.changed.fire()
  }

  dispose(): void {
    this.changed.dispose()
  }
}
