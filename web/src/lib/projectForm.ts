// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

import type { Project, ProjectInput } from '../api'
import { splitList } from './format'

export type ProjectTarget = 'any' | 'agent' | 'labels'

/** The settings shared by "new project" and "edit project" (the repository itself is fixed). */
export interface ProjectForm {
  name: string
  description: string
  target: ProjectTarget
  agent_id: string
  selector: string
  sandbox_image: string
  instructions: string
  trigger_label: string
}

export function projectFormFrom(p?: Project | null): ProjectForm {
  const selector = p?.selector ?? []
  return {
    name: p?.name ?? '',
    description: p?.description ?? '',
    target: p?.agent_id ? 'agent' : selector.length ? 'labels' : 'any',
    agent_id: p?.agent_id ?? '',
    selector: selector.join(', '),
    sandbox_image: p?.sandbox_image ?? '',
    instructions: p?.instructions ?? '',
    trigger_label: p?.trigger_label ?? '',
  }
}

export function projectInput(f: ProjectForm): ProjectInput {
  return {
    name: f.name.trim(),
    description: f.description.trim(),
    agent_id: f.target === 'agent' && f.agent_id ? f.agent_id : null,
    selector: f.target === 'labels' ? splitList(f.selector) : [],
    sandbox_image: f.sandbox_image.trim(),
    instructions: f.instructions,
    trigger_label: f.trigger_label.trim(),
  }
}

export function projectFormErrors(f: ProjectForm): Record<string, string> {
  const e: Record<string, string> = {}
  if (f.target === 'agent' && !f.agent_id) e.agent = 'Pick the agent, or choose "Any agent".'
  if (f.target === 'labels' && !splitList(f.selector).length) e.selector = 'Enter at least one label.'
  if (f.sandbox_image.trim() && /\s/.test(f.sandbox_image.trim())) e.sandbox = 'An image reference has no spaces, e.g. golang:1.27.'
  if (/\s/.test(f.trigger_label.trim())) e.trigger = 'Use a single label without spaces.'
  return e
}

/** Same repository-name rule as the server: letters, digits, dot, underscore, dash. */
export const REPO_NAME_RE = /^[A-Za-z0-9._-]{1,100}$/

/** A GitLab namespace path: up to 20 nested groups, no empty, . or .. parts (mirrors the server). */
export function validGitLabOwner(owner: string): boolean {
  const segs = owner.split('/')
  return owner.length <= 255 && segs.length <= 20 && segs.every((s) => s !== '.' && s !== '..' && REPO_NAME_RE.test(s))
}
