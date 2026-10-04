<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
// The "New task" dialog, shared by the task board and project pages.
import { ref, watch } from 'vue'
import { api, type Task, type TaskTemplate } from '../api'
import { useToast } from '../stores/toast'
import { taskFormFrom, taskFormValid, taskInput } from '../lib/taskForm'
import Modal from './Modal.vue'
import TaskFields from './TaskFields.vue'
import Icon from './Icon'

const props = defineProps<{
  open: boolean
  initial?: Partial<TaskTemplate> | null
  lockProject?: boolean
  title?: string
  /** Plans to link up front (Create task from a plan). */
  planIds?: string[]
  /** Focus the task on one phase of a plan (Create task on a phase). */
  focus?: { phaseId: string; phaseTitle: string; planId: string; planTitle: string } | null
}>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'created', t: Task): void }>()
const toast = useToast()

const fresh = () => ({ ...taskFormFrom(props.initial), plan_ids: [...(props.planIds ?? [])], plan_phase_id: props.focus?.phaseId ?? '' })
const form = ref(fresh())
const creating = ref(false)
const tried = ref(false)

watch(
  () => props.open,
  (o) => {
    if (!o) return
    form.value = fresh()
    tried.value = false
  },
)

async function create(draft = false) {
  tried.value = true
  if (!taskFormValid(form.value)) return
  creating.value = true
  try {
    const t = await api.createTask({ ...taskInput(form.value), ...(draft ? { draft: true } : {}) })
    toast.success(draft ? `Draft saved: ${t.title || 'untitled'}. Start it when you are ready.` : `Task queued: ${t.title || 'untitled'}`)
    emit('created', t)
  } catch {
    /* toasted */
  } finally {
    creating.value = false
  }
}
</script>

<template>
  <Modal :open="open" :title="title ?? 'New task'" wide :dismissable="!creating" @close="emit('close')">
    <div v-if="focus" class="banner info" role="note" style="margin-bottom: 14px">
      <Icon name="list" />
      <div class="banner-body">
        This task works on the phase <strong>{{ focus.phaseTitle }}</strong> of the plan <strong>{{ focus.planTitle }}</strong>. The agent sees the
        whole plan, but finishes and reports on this phase only.
      </div>
    </div>
    <form id="new-task" novalidate @submit.prevent="create()">
      <TaskFields v-model="form" id-prefix="nt" :show-errors="tried" :lock-project="lockProject" with-plans :locked-plan-id="focus?.planId" />
    </form>
    <template #footer>
      <button type="button" class="btn" @click="emit('close')">Cancel</button>
      <button type="button" class="btn" :disabled="creating" title="Save without starting; start it later from the task list" @click="create(true)">Save as draft</button>
      <button type="submit" form="new-task" class="btn btn-primary" :disabled="creating">
        <span v-if="creating" class="spinner" /><Icon v-else name="play" />{{ creating ? 'Queuing…' : 'Queue task' }}
      </button>
    </template>
  </Modal>
</template>
