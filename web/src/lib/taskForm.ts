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

import type { Autonomy, TaskInput, TaskTemplate } from '../api'
import { splitList } from './format'

/** 'project' = let the server apply the project's preferred agent / labels. */
export type TargetMode = 'any' | 'agent' | 'labels' | 'project'

export interface TaskForm {
  title: string
  goal: string
  target: TargetMode
  agent_id: string
  selector: string
  autonomy: Autonomy
  budget_usd: number
  timeout_sec: number
  max_turns: number
  max_attempts: number
  priority: number
  /** Coding task on a project's repository ('' = none). */
  project_id: string
  /** Project plans to link (new tasks on a project only). */
  plan_ids: string[]
  /** One phase of a plan to focus the task on ('' = whole plans). */
  plan_phase_id: string
}

export function taskFormFrom(t?: Partial<TaskTemplate> | null): TaskForm {
  const selector = t?.selector ?? []
  return {
    title: t?.title ?? '',
    goal: t?.goal ?? '',
    target: t?.agent_id ? 'agent' : selector.length ? 'labels' : t?.project_id ? 'project' : 'agent',
    agent_id: t?.agent_id ?? '',
    selector: selector.join(', '),
    // Project tasks default to L2 (edits, commits and PRs unattended; host shell still asks).
    autonomy: t?.autonomy ?? (t?.project_id ? 2 : 1),
    budget_usd: t?.budget_usd ?? 0,
    timeout_sec: t?.timeout_sec ?? 0,
    max_turns: t?.max_turns ?? 0,
    max_attempts: t?.max_attempts ?? 2,
    priority: t?.priority ?? 0,
    project_id: t?.project_id ?? '',
    plan_ids: [],
    plan_phase_id: '',
  }
}

export function taskInput(f: TaskForm): TaskInput & TaskTemplate {
  const n = (v: number) => (Number.isFinite(Number(v)) ? Number(v) : 0)
  return {
    title: f.title.trim(),
    goal: f.goal,
    agent_id: f.target === 'agent' && f.agent_id ? f.agent_id : null,
    selector: f.target === 'labels' ? splitList(f.selector) : [],
    autonomy: f.autonomy,
    budget_usd: n(f.budget_usd),
    timeout_sec: Math.round(n(f.timeout_sec)),
    max_turns: Math.round(n(f.max_turns)),
    max_attempts: Math.round(n(f.max_attempts)),
    priority: Math.round(n(f.priority)),
    project_id: f.project_id || null,
    ...(f.project_id && f.plan_ids.length ? { plan_ids: f.plan_ids } : {}),
    ...(f.project_id && f.plan_phase_id ? { plan_phase_id: f.plan_phase_id } : {}),
  }
}

export function taskFormValid(f: TaskForm): boolean {
  if (!f.goal.trim()) return false
  if (f.target === 'agent' && !f.agent_id) return false
  if (f.target === 'labels' && !splitList(f.selector).length) return false
  if (f.target === 'project' && !f.project_id) return false
  return true
}
