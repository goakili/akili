<!--
  Copyright (c) 2026 Jonas Kaninda.

  This file is part of Akili.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published by
  the Free Software Foundation, either version 3 of the License, or
  (at your option) any later version.

  This program is distributed in the hope that it will be useful,
  but WITHOUT ANY WARRANTY; without even the implied warranty of
  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
  GNU Affero General Public License for more details.

  You should have received a copy of the GNU Affero General Public License
  along with this program.  If not, see <https://www.gnu.org/licenses/>.

  SPDX-License-Identifier: AGPL-3.0-or-later
-->
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { useConfirm } from '../stores/confirm'
import Modal from './Modal.vue'

const confirm = useConfirm()
const { current } = storeToRefs(confirm)
const typed = ref('')
watch(current, () => (typed.value = ''))
const ok = computed(() => !current.value?.requireText || typed.value.trim() === current.value.requireText)
</script>

<template>
  <Modal :open="!!current" :title="current?.title ?? ''" @close="confirm.answer(false)">
    <form v-if="current" id="confirm-form" class="stack" @submit.prevent="ok && confirm.answer(true)">
      <p v-if="current.message" class="pre-wrap" style="margin: 0">{{ current.message }}</p>
      <div v-if="current.requireText" class="field">
        <label for="confirm-type">Type <code>{{ current.requireText }}</code> to confirm</label>
        <input id="confirm-type" v-model="typed" class="input mono" autocomplete="off" autofocus />
      </div>
    </form>
    <template #footer>
      <button type="button" class="btn" @click="confirm.answer(false)">Cancel</button>
      <button type="submit" form="confirm-form" class="btn" :class="current?.danger ? 'btn-danger' : 'btn-primary'" :disabled="!ok">
        {{ current?.confirmText ?? 'Confirm' }}
      </button>
    </template>
  </Modal>
</template>
