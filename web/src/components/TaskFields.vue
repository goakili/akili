<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { api, AUTONOMY_LEVELS, type Autonomy, type PlanSummary } from '../api'
import { useCatalog } from '../stores/catalog'
import { useCoder } from '../stores/coder'
import type { TargetMode, TaskForm } from '../lib/taskForm'
import { splitList } from '../lib/format'
import Icon, { type IconName } from './Icon'

const form = defineModel<TaskForm>({ required: true })
const props = defineProps<{
  idPrefix?: string
  showErrors?: boolean
  /** Fix the project (opened from a project page). */
  lockProject?: boolean
  /** Offer the project's plans (new tasks only; schedules don't link plans). */
  withPlans?: boolean
  /** A plan the task must keep (it focuses on one of its phases). */
  lockedPlanId?: string
}>()
const catalog = useCatalog()
const coder = useCoder()
const p = computed(() => props.idPrefix ?? 'tf')
const agents = computed(() =>
  catalog.agents.filter((a) => a.status !== 'revoked').sort((a, b) => Number(b.status === 'online') - Number(a.status === 'online') || a.name.localeCompare(b.name)),
)

onMounted(async () => {
  coder.loadProjects()
  await catalog.loadAgents()
  // Sensible default: the first online agent.
  if (form.value.target === 'agent' && !form.value.agent_id) {
    const first = agents.value.find((a) => a.status === 'online') ?? agents.value[0]
    if (first) form.value.agent_id = first.id
  }
})

const project = computed(() => coder.project(form.value.project_id))

// Only active plans can be linked; a plan picked before switching project is dropped.
const plans = ref<PlanSummary[]>([])
watch(
  () => (props.withPlans ? form.value.project_id : ''),
  async (id) => {
    plans.value = []
    if (!id) {
      form.value.plan_ids = []
      return
    }
    const all = (await api.listPlans(id, { quiet: true }).catch(() => null)) ?? []
    if (form.value.project_id !== id) return
    plans.value = all.filter((pl) => pl.status === 'active' || pl.status === 'in_progress')
    form.value.plan_ids = form.value.plan_ids.filter((pid) => plans.value.some((pl) => pl.id === pid))
  },
  { immediate: true },
)
function togglePlan(id: string) {
  const ids = new Set(form.value.plan_ids)
  if (ids.has(id)) ids.delete(id)
  else ids.add(id)
  form.value.plan_ids = [...ids]
}
const projectTarget = computed(() => {
  const pr = project.value
  if (!pr) return 'the project\'s preferred agent'
  if (pr.agent_id) return catalog.agentName(pr.agent_id)
  if (pr.selector?.length) return `agents labelled ${pr.selector.join(', ')}`
  return 'any available agent'
})
const TARGETS = computed(() => {
  const t: { value: TargetMode; title: string; sub: string; icon: IconName }[] = []
  if (form.value.project_id) t.push({ value: 'project', title: 'Project default', sub: `Runs on ${projectTarget.value}.`, icon: 'repo' })
  t.push(
    { value: 'agent', title: 'Specific agent', sub: 'Run on one host you pick.', icon: 'agents' },
    { value: 'labels', title: 'By labels', sub: 'Any agent carrying every label.', icon: 'layers' },
  )
  // Without a target a project task falls back to the project's default, so "any" only applies without one.
  if (!form.value.project_id) t.push({ value: 'any', title: 'Any agent', sub: 'First available agent.', icon: 'globe' })
  return t
})

// Picking a project defaults the target to the project's own; clearing it goes back to an agent.
watch(
  () => form.value.project_id,
  (id, old) => {
    if (id && !old && (form.value.target === 'agent' || form.value.target === 'any')) form.value.target = 'project'
    if (!id && (form.value.target === 'project' || form.value.target === 'any')) form.value.target = 'agent'
    // Follow the default for coding tasks unless the operator picked a level themselves.
    if (!autonomyTouched && !!id !== !!old) form.value.autonomy = id ? 2 : 1
  },
)
let autonomyTouched = false
const AUTONOMY_ICONS: IconName[] = ['eye', 'approvals', 'zap', 'sparkles']

function setAutonomy(v: Autonomy) {
  autonomyTouched = true
  form.value.autonomy = v
}

const goalErr = computed(() => props.showErrors && !form.value.goal.trim())
const agentErr = computed(() => props.showErrors && form.value.target === 'agent' && !form.value.agent_id)
const labelErr = computed(() => props.showErrors && form.value.target === 'labels' && !splitList(form.value.selector).length)
const selectedAgent = computed(() => catalog.agents.find((a) => a.id === form.value.agent_id))
</script>

<template>
  <div class="stack loose">
    <div class="field">
      <label :for="`${p}-goal`">Goal <span class="req" aria-hidden="true">*</span></label>
      <textarea
        :id="`${p}-goal`"
        v-model="form.goal"
        class="textarea"
        :class="{ invalid: goalErr }"
        rows="4"
        required
        :aria-invalid="goalErr ? 'true' : undefined"
        :aria-describedby="`${p}-goal-hint`"
        placeholder="e.g. Check disk usage on this host and summarise anything above 80%."
      />
      <span v-if="goalErr" :id="`${p}-goal-hint`" class="error-msg"><Icon name="alert" />Describe what the agent should achieve.</span>
      <span v-else :id="`${p}-goal-hint`" class="hint">Be specific about scope and what "done" looks like.</span>
    </div>
    <div class="field">
      <label :for="`${p}-title`">Title <span class="opt">(optional)</span></label>
      <input :id="`${p}-title`" v-model="form.title" class="input" maxlength="200" placeholder="Defaults to the first line of the goal" />
    </div>

    <div v-if="coder.projects.length || form.project_id" class="field">
      <label :for="`${p}-project`">Project <span class="opt">(optional)</span></label>
      <select :id="`${p}-project`" v-model="form.project_id" class="select" :disabled="lockProject" :aria-describedby="`${p}-project-hint`">
        <option value="">No project (host task)</option>
        <option v-for="pr in coder.projects" :key="pr.id" :value="pr.id">{{ pr.name }} · {{ pr.owner }}/{{ pr.repo }}</option>
      </select>
      <span v-if="project" :id="`${p}-project-hint`" class="hint">
        <Icon name="gitBranch" :size="12" style="vertical-align: -1px" /> The agent works on a branch <code>akili/&lt;task id&gt;</code> of {{ project.owner }}/{{ project.repo }} and opens a pull request into <code>{{ project.default_branch }}</code>. The default branch is never pushed to.
      </span>
      <span v-else :id="`${p}-project-hint`" class="hint">Pick a project to have the agent change its repository and open a pull request.</span>
    </div>

    <div v-if="withPlans && form.project_id && plans.length" class="field">
      <span :id="`${p}-plans`" class="label">Plans <span class="opt">(optional)</span></span>
      <div class="chips" role="group" :aria-labelledby="`${p}-plans`">
        <label v-for="pl in plans" :key="pl.id" class="chip-toggle" :class="{ on: form.plan_ids.includes(pl.id) }" :title="pl.description">
          <input type="checkbox" :checked="form.plan_ids.includes(pl.id)" :disabled="pl.id === lockedPlanId || (!form.plan_ids.includes(pl.id) && form.plan_ids.length >= 10)" @change="togglePlan(pl.id)" />
          <Icon v-if="form.plan_ids.includes(pl.id)" name="check" :size="13" />{{ pl.title }}
          <span class="muted">· {{ (pl.counts.done ?? 0) + (pl.counts.skipped ?? 0) }}/{{ pl.phases }}</span>
        </label>
      </div>
      <span class="hint">The agent gets the linked plans with their phases, works on the open phases and reports its progress on them.</span>
    </div>

    <div class="field">
      <span :id="`${p}-target`" class="label">Where should it run?</span>
      <div class="choices" role="radiogroup" :aria-labelledby="`${p}-target`">
        <label v-for="t in TARGETS" :key="t.value" class="choice" :class="{ on: form.target === t.value }">
          <input v-model="form.target" type="radio" :name="`${p}-target`" :value="t.value" />
          <Icon :name="t.icon" />
          <span><span class="c-title">{{ t.title }}</span><br /><span class="c-sub">{{ t.sub }}</span></span>
        </label>
      </div>
    </div>
    <div v-if="form.target === 'agent'" class="field">
      <label :for="`${p}-agent`">Agent</label>
      <select :id="`${p}-agent`" v-model="form.agent_id" class="select" :class="{ invalid: agentErr }" required>
        <option value="" disabled>Select an agent</option>
        <option v-for="a in agents" :key="a.id" :value="a.id">{{ a.name }} · {{ a.status }}{{ a.draining ? ' (draining)' : '' }}</option>
      </select>
      <span v-if="agentErr" class="error-msg"><Icon name="alert" />Pick the agent to run on.</span>
      <span v-else-if="!agents.length" class="hint warn">No agents yet. <RouterLink to="/agents">Add one</RouterLink> first.</span>
      <span v-else-if="selectedAgent && selectedAgent.status !== 'online'" class="hint warn">{{ selectedAgent.name }} is {{ selectedAgent.status }}; the task waits in the queue until it connects.</span>
      <span v-else-if="selectedAgent && !selectedAgent.policy_id" class="hint warn">{{ selectedAgent.name }} has no policy, so it cannot use any tools.</span>
    </div>
    <div v-else-if="form.target === 'labels'" class="field">
      <label :for="`${p}-sel`">Label selector</label>
      <input :id="`${p}-sel`" v-model="form.selector" class="input mono" :class="{ invalid: labelErr }" placeholder="prod, web" required />
      <span v-if="labelErr" class="error-msg"><Icon name="alert" />Enter at least one label.</span>
      <span v-else class="hint">Comma separated. The task runs on an agent carrying every label.</span>
    </div>

    <div class="field">
      <span :id="`${p}-aut`" class="label">Autonomy</span>
      <div class="choices" role="radiogroup" :aria-labelledby="`${p}-aut`" style="grid-template-columns: repeat(auto-fit, minmax(170px, 1fr))">
        <label v-for="l in AUTONOMY_LEVELS" :key="l.value" class="choice" :class="{ on: form.autonomy === l.value }">
          <input type="radio" :name="`${p}-aut`" :value="l.value" :checked="form.autonomy === l.value" @change="setAutonomy(l.value)" />
          <Icon :name="AUTONOMY_ICONS[l.value]" />
          <span><span class="c-title">{{ l.label }}</span><br /><span class="c-sub">{{ l.help }}</span></span>
        </label>
      </div>
      <span class="hint">Anything riskier than the level allows pauses for a human approval. Critical actions always need approval, and the agent's policy still applies.</span>
    </div>

    <details class="rule-block">
      <summary class="strong" style="cursor: pointer">Budget, limits &amp; retries</summary>
      <div class="grid-3" style="margin-top: 14px">
        <div class="field">
          <label :for="`${p}-budget`">Budget (USD)</label>
          <input :id="`${p}-budget`" v-model.number="form.budget_usd" class="input" type="number" min="0" step="0.01" />
          <span class="hint">Model spend cap. 0 = no cap.</span>
        </div>
        <div class="field">
          <label :for="`${p}-timeout`">Timeout (sec)</label>
          <input :id="`${p}-timeout`" v-model.number="form.timeout_sec" class="input" type="number" min="0" step="60" />
          <span class="hint">0 = no timeout.</span>
        </div>
        <div class="field">
          <label :for="`${p}-turns`">Max turns</label>
          <input :id="`${p}-turns`" v-model.number="form.max_turns" class="input" type="number" min="0" />
          <span class="hint">0 = no cap.</span>
        </div>
        <div class="field">
          <label :for="`${p}-attempts`">Max attempts</label>
          <input :id="`${p}-attempts`" v-model.number="form.max_attempts" class="input" type="number" min="0" />
        </div>
        <div class="field">
          <label :for="`${p}-prio`">Priority</label>
          <input :id="`${p}-prio`" v-model.number="form.priority" class="input" type="number" step="1" />
          <span class="hint">Higher runs first.</span>
        </div>
      </div>
    </details>
  </div>
</template>
