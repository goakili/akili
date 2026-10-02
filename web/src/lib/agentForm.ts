// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

import type { Agent, AgentInput, Autonomy } from '../api'
import { splitList } from './format'

export interface AgentForm {
  name: string
  description: string
  labels: string
  policy_id: string
  autonomy: Autonomy
  provider_id: string
  max_parallel: number
  instructions: string
  monthly_budget_usd: number
  git_name: string
  git_email: string
  skill_ids: string[]
}

export function agentFormFrom(a?: Agent | null): AgentForm {
  return {
    name: a?.name ?? '',
    description: a?.description ?? '',
    labels: (a?.labels ?? []).join(', '),
    policy_id: a?.policy_id ?? '',
    autonomy: a?.autonomy ?? 1,
    provider_id: a?.provider_id ?? '',
    max_parallel: a?.max_parallel ?? 2,
    instructions: a?.instructions ?? '',
    monthly_budget_usd: a?.monthly_budget_usd ?? 0,
    git_name: a?.git_name ?? '',
    git_email: a?.git_email ?? '',
    skill_ids: (a?.skills ?? []).map((s) => s.id),
  }
}

const sameList = (a: string[], b: string[]) => a.length === b.length && [...a].sort().join('\n') === [...b].sort().join('\n')

/**
 * Builds the request body. With `initial`, only changed fields are included (PATCH semantics: the
 * server leaves omitted fields alone; "" clears policy_id / provider_id).
 */
export function agentInput(f: AgentForm, initial?: AgentForm): AgentInput {
  const out: AgentInput = {}
  const labels = splitList(f.labels)
  const changed = <K extends keyof AgentForm>(k: K) => !initial || initial[k] !== f[k]
  if (changed('name')) out.name = f.name.trim()
  if (changed('description')) out.description = f.description
  if (!initial || !sameList(labels, splitList(initial.labels))) out.labels = labels
  if (changed('policy_id') && (initial || f.policy_id)) out.policy_id = f.policy_id
  if (changed('autonomy')) out.autonomy = f.autonomy
  if (changed('provider_id') && (initial || f.provider_id)) out.provider_id = f.provider_id
  if (changed('max_parallel')) out.max_parallel = Number(f.max_parallel)
  if (changed('instructions')) out.instructions = f.instructions
  if (changed('monthly_budget_usd')) out.monthly_budget_usd = Number(f.monthly_budget_usd) || 0
  if (changed('git_name') && (initial || f.git_name.trim())) out.git_name = f.git_name.trim()
  if (changed('git_email') && (initial || f.git_email.trim())) out.git_email = f.git_email.trim()
  if (!initial || !sameList(f.skill_ids, initial.skill_ids)) out.skill_ids = [...f.skill_ids]
  return out
}
