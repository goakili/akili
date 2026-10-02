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

const props = defineProps<{ open: boolean; initial?: Partial<TaskTemplate> | null; lockProject?: boolean; title?: string }>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'created', t: Task): void }>()
const toast = useToast()

const form = ref(taskFormFrom(props.initial))
const creating = ref(false)
const tried = ref(false)

watch(
  () => props.open,
  (o) => {
    if (!o) return
    form.value = taskFormFrom(props.initial)
    tried.value = false
  },
)

async function create() {
  tried.value = true
  if (!taskFormValid(form.value)) return
  creating.value = true
  try {
    const t = await api.createTask(taskInput(form.value))
    toast.success(`Task queued: ${t.title || 'untitled'}`)
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
    <form id="new-task" novalidate @submit.prevent="create"><TaskFields v-model="form" id-prefix="nt" :show-errors="tried" :lock-project="lockProject" /></form>
    <template #footer>
      <button type="button" class="btn" @click="emit('close')">Cancel</button>
      <button type="submit" form="new-task" class="btn btn-primary" :disabled="creating">
        <span v-if="creating" class="spinner" /><Icon v-else name="play" />{{ creating ? 'Queuing…' : 'Queue task' }}
      </button>
    </template>
  </Modal>
</template>
