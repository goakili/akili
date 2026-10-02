<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api, ApiError, type Skill } from '../api'
import { useAuth } from '../stores/auth'
import { useConfirm } from '../stores/confirm'
import { useToast } from '../stores/toast'
import { fmtDate } from '../lib/format'
import Icon from '../components/Icon'
import PageHeader from '../components/PageHeader.vue'
import EmptyState from '../components/EmptyState.vue'
import SafeMarkdown from '../components/SafeMarkdown'
import Badge from '../components/Badge.vue'

const auth = useAuth()
const confirm = useConfirm()
const toast = useToast()
const skills = ref<Skill[]>([])
const selected = ref<Skill | null>(null)
const creating = ref(false)
const name = ref('')
const description = ref('')
const content = ref('')
const saving = ref(false)
const loading = ref(true)

const editing = computed(() => creating.value || !!selected.value)
/** Built-in runbooks are maintained by Akili: read-only, duplicate to customise. */
const readonly = computed(() => !auth.isAdmin || (!!selected.value?.builtin && !creating.value))
const dirty = computed(() => {
  if (creating.value) return !!(name.value || content.value)
  const s = selected.value
  return !!s && (s.name !== name.value || s.description !== description.value || s.content !== content.value)
})

async function load() {
  try {
    skills.value = (await api.listSkills()) ?? []
  } catch {
    /* toasted */
  } finally {
    loading.value = false
  }
}

async function guardDirty(): Promise<boolean> {
  if (!dirty.value || readonly.value) return true
  return confirm.ask({ title: 'Discard unsaved changes?', confirmText: 'Discard', danger: true })
}

async function select(s: Skill) {
  if (!(await guardDirty())) return
  creating.value = false
  selected.value = s
  name.value = s.name
  description.value = s.description
  content.value = s.content
}

async function startNew() {
  if (!(await guardDirty())) return
  selected.value = null
  creating.value = true
  name.value = ''
  description.value = ''
  content.value = '# Skill name\n\nWhen to use this skill, then the exact procedure:\n\n1. …\n'
}

function duplicate() {
  const s = selected.value
  if (!s) return
  selected.value = null
  creating.value = true
  name.value = `${s.name} (custom)`
  description.value = s.description
  content.value = s.content
  toast.info(`Editing a copy of ${s.name}. Save to create it.`)
}

async function save() {
  if (readonly.value || !name.value.trim() || !content.value.trim()) return
  saving.value = true
  const body = { name: name.value.trim(), description: description.value, content: content.value }
  try {
    const s = creating.value ? await api.createSkill(body) : await api.updateSkill(selected.value!.id, body)
    toast.success(creating.value ? `Skill ${s.name} created` : `Saved ${s.name} (v${s.version})`)
    creating.value = false
    await load()
    selected.value = skills.value.find((x) => x.id === s.id) ?? s
    name.value = s.name
    description.value = s.description
    content.value = s.content
  } catch (e) {
    // A built-in runbook the list did not mark (older data) is refused: switch to read-only.
    if (e instanceof ApiError && e.status === 403 && selected.value) selected.value = { ...selected.value, builtin: true }
  } finally {
    saving.value = false
  }
}

async function remove() {
  const s = selected.value
  if (!s || s.builtin) return
  if (!(await confirm.ask({ title: `Delete skill ${s.name}?`, message: 'It is removed from every agent it is assigned to.', confirmText: `Delete ${s.name}`, danger: true }))) return
  try {
    await api.deleteSkill(s.id)
    selected.value = null
    toast.success('Skill deleted')
    load()
  } catch (e) {
    if (e instanceof ApiError && e.status === 403) selected.value = { ...s, builtin: true }
  }
}

onMounted(load)
</script>

<template>
  <div>
    <PageHeader title="Skills" subtitle="Markdown procedures added to an agent’s system prompt. Edits bump the version; running sessions keep theirs.">
      <button v-if="auth.isAdmin" type="button" class="btn btn-primary" @click="startNew"><Icon name="plus" />New skill</button>
    </PageHeader>

    <div class="split">
      <nav class="card side-list" style="padding: 6px" aria-label="Skills">
        <template v-if="loading">
          <div v-for="i in 3" :key="i" class="list-row"><span class="skel" style="width: 70%" /></div>
        </template>
        <div v-else-if="!skills.length" class="empty compact"><strong>No skills yet</strong><p class="small">Skills teach agents repeatable procedures.</p></div>
        <button
          v-for="s in skills"
          :key="s.id"
          type="button"
          class="list-row"
          :class="{ active: selected?.id === s.id }"
          :aria-current="selected?.id === s.id ? 'true' : undefined"
          @click="select(s)"
        >
          <div class="grow">
            <div class="truncate strong">{{ s.name }}</div>
            <div class="xs muted truncate">{{ s.description || 'No description' }}</div>
          </div>
          <span v-if="s.builtin" class="badge violet square" title="Runbook · built-in (read-only)"><Icon name="skills" />runbook</span>
          <span v-else class="badge outline square">v{{ s.version }}</span>
        </button>
      </nav>

      <div v-if="!editing" class="card">
        <EmptyState title="Select a skill" icon="skills">
          Pick a skill on the left{{ auth.isAdmin ? ' or create a new one' : '' }}. Skills are attached to agents on their configuration tab.
          <template v-if="auth.isAdmin" #actions><button type="button" class="btn btn-primary btn-sm" @click="startNew"><Icon name="plus" />New skill</button></template>
        </EmptyState>
      </div>
      <form v-else class="card" @submit.prevent="save">
        <div class="card-head" style="flex-wrap: wrap">
          <div class="row wrap">
            <h2>{{ creating ? 'New skill' : selected?.name }}</h2>
            <Badge v-if="selected?.builtin && !creating" value="runbook" />
          </div>
          <div class="row wrap">
            <span v-if="selected && !creating" class="small muted">
              v{{ selected.version }} · <span class="mono" :title="selected.hash">{{ selected.hash.slice(0, 12) }}</span> · updated {{ fmtDate(selected.updated_at) }}
            </span>
            <button v-if="selected && !creating && auth.isAdmin" type="button" class="btn btn-sm" :class="{ 'btn-primary': selected.builtin }" @click="duplicate">
              <Icon name="copy" />Duplicate{{ selected.builtin ? ' to edit' : '' }}
            </button>
          </div>
        </div>
        <div v-if="selected?.builtin && !creating" class="banner info" style="margin: 14px 18px 0; border-radius: var(--radius)">
          <Icon name="lock" />
          <div class="banner-body">Built-in runbooks are maintained by Akili and updated with it, so they are read-only. Duplicate this one to adapt it; attach either to agents on their configuration tab.</div>
        </div>
        <fieldset class="card-body stack" :disabled="readonly || saving" style="border: 0; margin: 0">
          <div class="grid-2">
            <div class="field">
              <label for="sk-name">Name <span class="req">*</span></label>
              <input id="sk-name" v-model="name" class="input" required maxlength="120" />
            </div>
            <div class="field">
              <label for="sk-desc">Description</label>
              <input id="sk-desc" v-model="description" class="input" maxlength="500" />
            </div>
          </div>
          <div class="grid-2" style="align-items: stretch">
            <div class="field">
              <label for="sk-content">Content (Markdown) <span class="req">*</span></label>
              <textarea id="sk-content" v-model="content" class="textarea mono" rows="22" required spellcheck="false" style="flex: 1" />
            </div>
            <div class="field">
              <span class="label">Preview</span>
              <div class="rule-block" style="flex: 1; overflow: auto; max-height: 520px; background: var(--bg-primary)"><SafeMarkdown :text="content" /></div>
            </div>
          </div>
          <div v-if="!readonly" class="row between">
            <button v-if="selected && !creating" type="button" class="btn btn-danger-ghost" @click="remove"><Icon name="trash" />Delete</button>
            <span v-else />
            <button type="submit" class="btn btn-primary" :disabled="!dirty || !name.trim() || !content.trim()">
              <span v-if="saving" class="spinner" />{{ saving ? 'Saving…' : creating ? 'Create skill' : 'Save changes' }}
            </button>
          </div>
        </fieldset>
      </form>
    </div>
  </div>
</template>
