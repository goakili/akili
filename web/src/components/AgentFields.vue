<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api, ApiError, type ModelProvider } from '../api'
import { useCatalog } from '../stores/catalog'
import type { AgentForm } from '../lib/agentForm'
import AutonomySelect from './AutonomySelect.vue'
import Icon from './Icon'

const form = defineModel<AgentForm>({ required: true })
defineProps<{ disabled?: boolean; showErrors?: boolean; gitDefaultName?: string; gitDefaultEmail?: string }>()
const catalog = useCatalog()
const providers = ref<ModelProvider[]>([])
const providersDenied = ref(false)

onMounted(async () => {
  catalog.loadPolicies().catch(() => {})
  catalog.loadSkills().catch(() => {})
  try {
    providers.value = (await api.listProviders({ quiet: true })) ?? []
  } catch (e) {
    if (e instanceof ApiError && e.status === 403) providersDenied.value = true
  }
})

function toggleSkill(id: string) {
  const s = new Set(form.value.skill_ids)
  if (s.has(id)) s.delete(id)
  else s.add(id)
  form.value.skill_ids = [...s]
}
</script>

<template>
  <fieldset class="stack loose" :disabled="disabled" style="border: 0; padding: 0; margin: 0; min-width: 0">
    <div class="grid-2">
      <div class="field">
        <label for="ag-name">Name <span class="req" aria-hidden="true">*</span></label>
        <input
          id="ag-name"
          v-model="form.name"
          class="input"
          :class="{ invalid: showErrors && !form.name.trim() }"
          required
          maxlength="120"
          placeholder="web-01"
          :aria-invalid="showErrors && !form.name.trim() ? 'true' : undefined"
          aria-describedby="ag-name-err"
        />
        <span v-if="showErrors && !form.name.trim()" id="ag-name-err" class="error-msg"><Icon name="alert" />Give the agent a name.</span>
      </div>
      <div class="field">
        <label for="ag-labels">Labels <span class="opt">(optional)</span></label>
        <input id="ag-labels" v-model="form.labels" class="input mono" placeholder="prod, eu-west, web" aria-describedby="ag-labels-hint" />
        <span id="ag-labels-hint" class="hint">Comma separated. Tasks can target agents by label.</span>
      </div>
      <div class="field span-all">
        <label for="ag-desc">Description <span class="opt">(optional)</span></label>
        <input id="ag-desc" v-model="form.description" class="input" maxlength="500" placeholder="What this host is for" />
      </div>
    </div>

    <div class="stack">
      <div class="section-title">Guardrails</div>
      <div class="grid-2">
        <div class="field">
          <label for="ag-policy">Policy</label>
          <select id="ag-policy" v-model="form.policy_id" class="select" :class="{ warn: !form.policy_id }" aria-describedby="ag-policy-hint">
            <option value="">None: no tools at all</option>
            <option v-for="p in catalog.policies" :key="p.id" :value="p.id">{{ p.name }}{{ p.builtin ? ' (built-in)' : '' }}</option>
          </select>
          <p v-if="!form.policy_id" id="ag-policy-hint" class="hint warn">
            Without a policy the agent can only talk: every tool call is denied. Pick one, e.g. <em>read-only</em> or <em>operator-safe</em>.
          </p>
          <p v-else id="ag-policy-hint" class="hint">Every tool call is checked against this policy on the control plane.</p>
        </div>
        <AutonomySelect id="ag-autonomy" v-model="form.autonomy" />
      </div>
    </div>

    <div class="stack">
      <div class="section-title">Model &amp; limits</div>
      <div class="grid-3">
        <div class="field">
          <label for="ag-provider">Model provider</label>
          <select id="ag-provider" v-model="form.provider_id" class="select" :disabled="providersDenied">
            <option value="">Organization default</option>
            <option v-for="p in providers" :key="p.id" :value="p.id">{{ p.name }} · {{ p.model }}</option>
          </select>
          <span v-if="providersDenied" class="hint">Only admins can list providers; the default is used.</span>
        </div>
        <div class="field">
          <label for="ag-par">Max parallel sessions</label>
          <input id="ag-par" v-model.number="form.max_parallel" class="input" type="number" min="1" max="32" />
        </div>
        <div class="field">
          <label for="ag-budget">Monthly budget (USD)</label>
          <input id="ag-budget" v-model.number="form.monthly_budget_usd" class="input" type="number" min="0" step="0.01" aria-describedby="ag-budget-hint" />
          <span id="ag-budget-hint" class="hint">0 = no limit</span>
        </div>
      </div>
    </div>

    <div class="stack">
      <div class="section-title">Git identity</div>
      <div class="grid-2">
        <div class="field">
          <label for="ag-git-name">Commit name <span class="opt">(optional)</span></label>
          <input id="ag-git-name" v-model="form.git_name" class="input" maxlength="120" :placeholder="gitDefaultName ?? 'Server default'" />
        </div>
        <div class="field">
          <label for="ag-git-email">Commit email <span class="opt">(optional)</span></label>
          <input id="ag-git-email" v-model="form.git_email" class="input mono" type="email" maxlength="254" :placeholder="gitDefaultEmail ?? 'Server default'" aria-describedby="ag-git-email-hint" />
        </div>
        <p id="ag-git-email-hint" class="hint span-all">
          Commits and pushes use this identity, and the control plane refuses pushed commits with any other author. Use the email of a bot
          account on your forge (e.g. its noreply address) so commits link to it. A person's email is not allowed.
        </p>
      </div>
    </div>

    <div class="stack">
      <div class="section-title">Behaviour</div>
      <div class="field">
        <label for="ag-instr">Instructions <span class="opt">(optional)</span></label>
        <textarea id="ag-instr" v-model="form.instructions" class="textarea" rows="4" placeholder="Extra system-prompt guidance for this agent" />
      </div>
      <div class="field">
        <span id="ag-skills" class="label">Skills</span>
        <div v-if="!catalog.skills.length" class="hint">No skills defined yet. <RouterLink to="/skills">Create one</RouterLink> to give agents reusable procedures.</div>
        <div class="chips" role="group" aria-labelledby="ag-skills">
          <label v-for="s in catalog.skills" :key="s.id" class="chip-toggle" :class="{ on: form.skill_ids.includes(s.id) }" :title="s.description">
            <input type="checkbox" :checked="form.skill_ids.includes(s.id)" @change="toggleSkill(s.id)" />
            <Icon v-if="form.skill_ids.includes(s.id)" name="check" :size="13" />{{ s.name }}
          </label>
        </div>
      </div>
    </div>
  </fieldset>
</template>
