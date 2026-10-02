// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api, type Agent, type Policy, type Skill, type ToolSpec } from '../api'

/** Small caches for lookups shown across pages (agent names, policy names). */
export const useCatalog = defineStore('catalog', () => {
  const agents = ref<Agent[]>([])
  const policies = ref<Policy[]>([])
  const skills = ref<Skill[]>([])
  const tools = ref<ToolSpec[]>([])
  let toolsInflight: Promise<void> | null = null
  let agentsAt = 0
  let agentsInflight: Promise<void> | null = null

  async function loadAgents(force = false): Promise<void> {
    if (!force && Date.now() - agentsAt < 5000 && agents.value.length) return
    if (agentsInflight) return agentsInflight
    agentsInflight = (async () => {
      try {
        agents.value = (await api.listAgents({ quiet: true })) ?? []
        agentsAt = Date.now()
      } catch {
        /* keep previous */
      } finally {
        agentsInflight = null
      }
    })()
    return agentsInflight
  }

  async function loadPolicies(): Promise<void> {
    policies.value = (await api.listPolicies()) ?? []
  }

  async function loadSkills(): Promise<void> {
    skills.value = (await api.listSkills()) ?? []
  }

  /** The tool catalog never changes at runtime: fetch it once. */
  function loadTools(): Promise<void> {
    if (tools.value.length) return Promise.resolve()
    toolsInflight ??= api
      .listTools({ quiet: true })
      .then((t) => void (tools.value = t ?? []))
      .catch(() => {})
      .finally(() => (toolsInflight = null))
    return toolsInflight
  }

  function toolRisk(name: string): string {
    return tools.value.find((t) => t.name === name)?.risk ?? ''
  }

  /** The policy bound to an agent (from the loaded list), if any. */
  function agentPolicy(a: Agent | null | undefined): Policy | undefined {
    if (!a?.policy_id) return undefined
    return policies.value.find((p) => p.id === a.policy_id)
  }

  function agentName(id: string | null | undefined): string {
    if (!id) return '—'
    return agents.value.find((a) => a.id === id)?.name ?? id
  }

  function policyName(id: string | null | undefined): string {
    if (!id) return 'none'
    return policies.value.find((p) => p.id === id)?.name ?? id
  }

  function upsertAgent(a: Agent) {
    const i = agents.value.findIndex((x) => x.id === a.id)
    if (i >= 0) agents.value[i] = a
    else agents.value.push(a)
  }

  function patchAgentStatus(id: string, status: string) {
    const a = agents.value.find((x) => x.id === id)
    if (a) a.status = status as Agent['status']
  }

  return { agents, policies, skills, tools, loadAgents, loadPolicies, loadSkills, loadTools, toolRisk, agentPolicy, agentName, policyName, upsertAgent, patchAgentStatus }
})
