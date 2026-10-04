<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
// Connect an existing repository, or create one on the forge (optionally scaffolded from a template).
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { api, ApiError, AUTONOMY_LEVELS, type Autonomy, type Integration, type ProjectInput, type ProjectTemplate } from '../api'
import { useCoder } from '../stores/coder'
import { useToast } from '../stores/toast'
import { projectFormErrors, projectFormFrom, projectInput, REPO_NAME_RE, validGitLabOwner } from '../lib/projectForm'
import { forgeLabel } from '../lib/forge'
import Modal from './Modal.vue'
import ProjectFields from './ProjectFields.vue'
import Icon from './Icon'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ (e: 'close'): void }>()
const coder = useCoder()
const toast = useToast()
const router = useRouter()

type Mode = 'connect' | 'create'
const mode = ref<Mode>('connect')
const integrationId = ref('')
const owner = ref('')
const repo = ref('')
const isPrivate = ref(true)
const templateId = ref('empty')
const autonomy = ref<Autonomy>(2)
const form = ref(projectFormFrom())
const tried = ref(false)
const saving = ref(false)
const formError = ref('')
const loadingData = ref(false)

const integrations = computed<Integration[]>(() => coder.forges)
const integration = computed(() => integrations.value.find((i) => i.id === integrationId.value))
const isGitLab = computed(() => integration.value?.kind === 'gitlab')
// A GitLab project access token can't create projects; GitLab would refuse with a 403.
const canCreate = computed(() => !(isGitLab.value && integration.value?.token_kind === 'project'))
watch(canCreate, (c) => {
  if (!c) mode.value = 'connect'
})
const templates = computed<ProjectTemplate[]>(() => coder.templates?.templates ?? [])
const template = computed(() => (mode.value === 'create' ? templates.value.find((t) => t.id === templateId.value) : undefined))

watch(
  () => props.open,
  async (o) => {
    if (!o) return
    mode.value = 'connect'
    owner.value = ''
    repo.value = ''
    isPrivate.value = true
    templateId.value = 'empty'
    autonomy.value = 2
    form.value = projectFormFrom()
    tried.value = false
    formError.value = ''
    loadingData.value = true
    await Promise.all([coder.loadIntegrations(), coder.loadTemplates()])
    loadingData.value = false
    integrationId.value = integrations.value[0]?.id ?? ''
    if (templates.value.length && !templates.value.some((t) => t.id === templateId.value)) templateId.value = templates.value[0].id
  },
  { immediate: true },
)

// New repositories default to the integration's user as owner.
watch([integration, mode], ([it, m], [prevIt]) => {
  if (m !== 'create' || !it) return
  if (!owner.value || owner.value === prevIt?.username) owner.value = it.username
})

const errors = computed(() => {
  const e: Record<string, string> = { ...projectFormErrors(form.value) }
  if (!integrationId.value) e.integration = 'Pick an integration.'
  const o = owner.value.trim().replace(/^\/+|\/+$/g, '')
  if (isGitLab.value) {
    if (!validGitLabOwner(o)) e.owner = o ? 'A group path such as platform/backend: letters, digits, dot, dash and underscore, separated by /.' : 'Enter the group or user (group/subgroup).'
  } else if (!REPO_NAME_RE.test(o)) e.owner = o ? 'Letters, digits, dot, dash and underscore only.' : 'Enter the owner (user or organization).'
  if (!REPO_NAME_RE.test(repo.value.trim())) e.repo = repo.value.trim() ? 'Letters, digits, dot, dash and underscore only.' : 'Enter the repository name.'
  return e
})
const err = (k: string) => (tried.value ? errors.value[k] : '')

async function submit() {
  tried.value = true
  formError.value = ''
  if (Object.keys(errors.value).length) return
  saving.value = true
  const body: ProjectInput = {
    ...projectInput(form.value),
    integration_id: integrationId.value,
    owner: owner.value.trim().replace(/^\/+|\/+$/g, ''),
    repo: repo.value.trim(),
  }
  if (mode.value === 'create') {
    body.create_repo = true
    body.private = isPrivate.value
    if (template.value && template.value.id !== 'empty') body.template = template.value.id
    if (template.value?.goal) body.autonomy = autonomy.value
  }
  try {
    const res = await api.createProject(body, { quiet: true })
    coder.upsertProject(res.project)
    emit('close')
    if (res.task) {
      toast.success('Scaffold task queued')
      router.push(`/tasks/${res.task.id}`)
    } else {
      toast.success(mode.value === 'create' ? `Created ${res.project.owner}/${res.project.repo}` : `Connected ${res.project.owner}/${res.project.repo}`)
      router.push(`/projects/${res.project.id}`)
    }
  } catch (e) {
    formError.value = e instanceof ApiError ? e.message : 'The project could not be created.'
  } finally {
    saving.value = false
  }
}

const TEMPLATE_ICON = (t: ProjectTemplate) => (t.goal ? 'sparkles' : 'file')
</script>

<template>
  <Modal :open="open" title="New project" wide :dismissable="!saving" @close="emit('close')">
    <div v-if="loadingData && !integrations.length" class="stack" aria-busy="true"><span class="skel" style="width: 60%" /><span class="skel" style="width: 80%" /></div>
    <div v-else-if="!integrations.length" class="stack">
      <div class="banner info">
        <Icon name="plug" />
        <div class="banner-body">A project lives on a git forge. <strong>Add an integration first</strong> (Gitea, GitHub or GitLab), then come back to connect or create a repository.</div>
      </div>
    </div>
    <form v-else id="new-project" class="stack loose" novalidate @submit.prevent="submit">
      <div v-if="formError" class="banner danger" role="alert"><Icon name="alert" /><div class="banner-body">{{ formError }}</div></div>

      <div class="segmented" role="radiogroup" aria-label="Repository" style="align-self: flex-start">
        <button type="button" role="radio" :aria-checked="mode === 'connect'" :class="{ on: mode === 'connect' }" @click="mode = 'connect'"><Icon name="link" />Connect existing repository</button>
        <button
          type="button"
          role="radio"
          :aria-checked="mode === 'create'"
          :class="{ on: mode === 'create' }"
          :disabled="!canCreate"
          :title="canCreate ? undefined : 'A GitLab project access token cannot create projects. Use a group token with the Maintainer role, or a personal token.'"
          @click="mode = 'create'"
        >
          <Icon name="plus" />Create new repository
        </button>
      </div>

      <div class="grid-3 repo-grid">
        <div class="field">
          <label for="np-int">Integration <span class="req">*</span></label>
          <select id="np-int" v-model="integrationId" class="select" :class="{ invalid: err('integration') }">
            <option v-for="i in integrations" :key="i.id" :value="i.id">{{ i.name }} · {{ forgeLabel(i.kind) }}</option>
          </select>
          <span v-if="err('integration')" class="error-msg"><Icon name="alert" />{{ err('integration') }}</span>
        </div>
        <div class="field">
          <label for="np-owner">Owner <span class="req">*</span></label>
          <input id="np-owner" v-model="owner" class="input mono" :class="{ invalid: err('owner') }" autocomplete="off" :placeholder="integration?.username || (isGitLab ? 'group/subgroup' : 'user or org')" />
          <span v-if="err('owner')" class="error-msg"><Icon name="alert" />{{ err('owner') }}</span>
        </div>
        <div class="field">
          <label for="np-repo">{{ mode === 'create' ? 'Repository name' : 'Repository' }} <span class="req">*</span></label>
          <input id="np-repo" v-model="repo" class="input mono" :class="{ invalid: err('repo') }" autocomplete="off" :placeholder="mode === 'create' ? 'orders-api' : 'existing-repo'" />
          <span v-if="err('repo')" class="error-msg"><Icon name="alert" />{{ err('repo') }}</span>
        </div>
      </div>
      <p v-if="mode === 'connect'" class="hint" style="margin-top: -8px">The integration's credentials must be able to read and push to it. Akili only ever pushes <code>akili/*</code> branches.</p>

      <template v-if="mode === 'create'">
        <label class="switch"><input v-model="isPrivate" type="checkbox" />Private repository</label>
        <div class="field">
          <span id="np-tpl" class="label">Template</span>
          <div class="tpl-grid" role="radiogroup" aria-labelledby="np-tpl">
            <label v-for="t in templates" :key="t.id" class="choice tpl" :class="{ on: templateId === t.id }">
              <input v-model="templateId" type="radio" name="np-tpl" :value="t.id" />
              <Icon :name="TEMPLATE_ICON(t)" />
              <span>
                <span class="c-title">{{ t.name }}</span>
                <span v-if="t.sandbox_image" class="label-chip" style="margin-left: 6px">{{ t.sandbox_image }}</span>
                <br /><span class="c-sub">{{ t.description }}</span>
              </span>
            </label>
          </div>
        </div>
        <div v-if="template?.goal" class="banner info">
          <Icon name="sparkles" />
          <div class="banner-body stack tight">
            <span>After creating the repository, Akili queues a <strong>scaffold task</strong>: an agent builds the {{ template.name }} layout on a branch, runs the tests in the sandbox and opens a pull request.</span>
            <div class="row wrap" style="gap: 8px">
              <label for="np-aut" class="strong small">Autonomy of the scaffold task</label>
              <select id="np-aut" v-model.number="autonomy" class="select" style="width: auto; min-width: 200px">
                <option v-for="l in AUTONOMY_LEVELS" :key="l.value" :value="l.value">{{ l.label }}</option>
              </select>
            </div>
          </div>
        </div>
      </template>

      <div class="section-title">Settings</div>
      <ProjectFields
        v-model="form"
        id-prefix="np"
        :show-errors="tried"
        :name-placeholder="repo.trim() || undefined"
        :sandbox-placeholder="template?.sandbox_image ? `${template.sandbox_image} (from the template)` : undefined"
        :instructions-placeholder="template?.instructions ? 'Empty uses the template\'s conventions' : undefined"
      />
    </form>

    <template #footer>
      <button type="button" class="btn" @click="emit('close')">Cancel</button>
      <RouterLink v-if="!integrations.length && !loadingData" to="/integrations" class="btn btn-primary" @click="emit('close')"><Icon name="plug" />Add an integration</RouterLink>
      <button v-else type="submit" form="new-project" class="btn btn-primary" :disabled="saving || loadingData">
        <span v-if="saving" class="spinner" /><Icon v-else :name="mode === 'create' ? 'plus' : 'link'" />
        {{ saving ? (mode === 'create' ? 'Creating…' : 'Connecting…') : mode === 'create' ? (template?.goal ? 'Create & scaffold' : 'Create repository') : 'Connect repository' }}
      </button>
    </template>
  </Modal>
</template>

<style scoped>
.repo-grid {
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) minmax(0, 1.2fr);
}
.tpl-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 10px;
}
.tpl .c-sub {
  display: inline-block;
  margin-top: 2px;
}
@media (max-width: 720px) {
  .repo-grid {
    grid-template-columns: 1fr;
  }
}
</style>
