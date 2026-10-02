<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
// Project settings shared by the "new project" dialog and the project's edit form.
import { computed, onMounted } from 'vue'
import { useCatalog } from '../stores/catalog'
import { projectFormErrors, type ProjectForm, type ProjectTarget } from '../lib/projectForm'
import Icon, { type IconName } from './Icon'

const form = defineModel<ProjectForm>({ required: true })
const props = defineProps<{ idPrefix?: string; showErrors?: boolean; namePlaceholder?: string; sandboxPlaceholder?: string; instructionsPlaceholder?: string }>()
const catalog = useCatalog()
const p = computed(() => props.idPrefix ?? 'pf')
const agents = computed(() => catalog.agents.filter((a) => a.status !== 'revoked').sort((a, b) => a.name.localeCompare(b.name)))
onMounted(() => catalog.loadAgents())

const TARGETS: { value: ProjectTarget; title: string; sub: string; icon: IconName }[] = [
  { value: 'any', title: 'Any agent', sub: 'First available agent.', icon: 'globe' },
  { value: 'agent', title: 'Preferred agent', sub: 'One host you pick.', icon: 'agents' },
  { value: 'labels', title: 'By labels', sub: 'Agents carrying every label.', icon: 'layers' },
]
const errors = computed(() => (props.showErrors ? projectFormErrors(form.value) : {}))
</script>

<template>
  <div class="grid-2">
    <div class="field">
      <label :for="`${p}-name`">Display name <span class="opt">(optional)</span></label>
      <input :id="`${p}-name`" v-model="form.name" class="input" maxlength="120" :placeholder="namePlaceholder || 'Defaults to the repository name'" />
    </div>
    <div class="field">
      <label :for="`${p}-trigger`">Trigger label <span class="opt">(optional)</span></label>
      <input :id="`${p}-trigger`" v-model="form.trigger_label" class="input mono" :class="{ invalid: errors.trigger }" maxlength="60" placeholder="akili" />
      <span v-if="errors.trigger" class="error-msg"><Icon name="alert" />{{ errors.trigger }}</span>
      <span v-else class="hint">An issue with this label becomes a coding task (needs the integration's webhook). Empty disables.</span>
    </div>
    <div class="field span-all">
      <label :for="`${p}-desc`">Description <span class="opt">(optional)</span></label>
      <input :id="`${p}-desc`" v-model="form.description" class="input" maxlength="500" placeholder="What this repository is" />
    </div>

    <div class="field span-all">
      <span :id="`${p}-target`" class="label">Which agent works on it?</span>
      <div class="choices" role="radiogroup" :aria-labelledby="`${p}-target`">
        <label v-for="t in TARGETS" :key="t.value" class="choice" :class="{ on: form.target === t.value }">
          <input v-model="form.target" type="radio" :name="`${p}-target`" :value="t.value" />
          <Icon :name="t.icon" />
          <span><span class="c-title">{{ t.title }}</span><br /><span class="c-sub">{{ t.sub }}</span></span>
        </label>
      </div>
      <span class="hint">The default for the project's tasks; a task can still pick another agent.</span>
    </div>
    <div v-if="form.target === 'agent'" class="field span-all">
      <label :for="`${p}-agent`">Preferred agent</label>
      <select :id="`${p}-agent`" v-model="form.agent_id" class="select" :class="{ invalid: errors.agent }">
        <option value="" disabled>Select an agent</option>
        <option v-for="a in agents" :key="a.id" :value="a.id">{{ a.name }} · {{ a.status }}</option>
      </select>
      <span v-if="errors.agent" class="error-msg"><Icon name="alert" />{{ errors.agent }}</span>
      <span v-else-if="!agents.length" class="hint warn">No agents yet. <RouterLink to="/agents">Add one</RouterLink> first.</span>
    </div>
    <div v-else-if="form.target === 'labels'" class="field span-all">
      <label :for="`${p}-sel`">Label selector</label>
      <input :id="`${p}-sel`" v-model="form.selector" class="input mono" :class="{ invalid: errors.selector }" placeholder="coder, go" />
      <span v-if="errors.selector" class="error-msg"><Icon name="alert" />{{ errors.selector }}</span>
      <span v-else class="hint">Comma separated.</span>
    </div>

    <div class="field span-all">
      <label :for="`${p}-img`">Sandbox image <span class="opt">(optional)</span></label>
      <input :id="`${p}-img`" v-model="form.sandbox_image" class="input mono" :class="{ invalid: errors.sandbox }" maxlength="200" :placeholder="sandboxPlaceholder || 'golang:1.26'" />
      <span v-if="errors.sandbox" class="error-msg"><Icon name="alert" />{{ errors.sandbox }}</span>
      <span v-else class="hint"><Icon name="box" :size="12" style="vertical-align: -1px" /> Builds and tests run in a disposable container from this image (the <code>sandbox_exec</code> tool), with the workspace mounted and no host access. Empty disables the sandbox.</span>
    </div>
    <div class="field span-all">
      <label :for="`${p}-instr`">Instructions <span class="opt">(optional)</span></label>
      <textarea
        :id="`${p}-instr`"
        v-model="form.instructions"
        class="textarea"
        rows="4"
        :placeholder="instructionsPlaceholder || 'Project conventions: layout, how to run the tests, commit style, what not to touch…'"
      />
      <span class="hint">Added to the system prompt of every session on this project.</span>
    </div>
  </div>
</template>
