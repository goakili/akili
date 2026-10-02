<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api, ROLES, roleRank, type Role, type User } from '../../api'
import { useAuth } from '../../stores/auth'
import { useToast } from '../../stores/toast'
import { relTime } from '../../lib/format'
import { useNow } from '../../lib/now'
import Modal from '../../components/Modal.vue'
import Avatar from '../../components/Avatar.vue'
import Icon from '../../components/Icon'
import SkeletonRows from '../../components/SkeletonRows.vue'

const auth = useAuth()
const toast = useToast()
const now = useNow()
const users = ref<User[]>([])
const loading = ref(true)
const showNew = ref(false)
const nf = ref({ email: '', name: '', role: 'operator' as Role, password: '' })
const saving = ref(false)
const resetFor = ref<User | null>(null)
const newPassword = ref('')

const grantable = () => ROLES.filter((r) => roleRank(r) <= roleRank(auth.role))
const editable = (u: User) => roleRank(u.role) <= roleRank(auth.role) && u.id !== auth.user?.id

async function load() {
  try {
    users.value = (await api.listUsers()) ?? []
  } catch {
    /* toasted */
  } finally {
    loading.value = false
  }
}

async function create() {
  saving.value = true
  try {
    await api.createUser({ ...nf.value, email: nf.value.email.trim() })
    showNew.value = false
    toast.success(`User ${nf.value.email.trim()} created`)
    load()
  } catch {
    /* toasted */
  } finally {
    saving.value = false
  }
}

async function update(u: User, body: { role?: Role; active?: boolean }) {
  try {
    const nu = await api.updateUser(u.id, body)
    Object.assign(u, nu)
    toast.success(`${u.email} updated`)
  } catch {
    load()
  }
}

async function resetPassword() {
  if (!resetFor.value) return
  saving.value = true
  try {
    await api.updateUser(resetFor.value.id, { password: newPassword.value })
    toast.success(`Password reset for ${resetFor.value.email}`)
    resetFor.value = null
  } catch {
    /* toasted */
  } finally {
    saving.value = false
  }
}

function openNew() {
  nf.value = { email: '', name: '', role: 'operator', password: '' }
  showNew.value = true
}

onMounted(load)
</script>

<template>
  <div class="stack">
    <div class="row end">
      <button type="button" class="btn btn-primary" @click="openNew"><Icon name="plus" />Add user</button>
    </div>
    <div class="card">
      <div class="table-wrap">
        <table class="table">
          <thead><tr><th>User</th><th>Role</th><th>Active</th><th class="hide-mobile">Last login</th><th><span class="sr-only">Actions</span></th></tr></thead>
          <tbody>
            <SkeletonRows v-if="loading" :cols="5" :rows="2" />
            <tr v-for="u in users" :key="u.id">
              <td>
                <div class="row" style="gap: 10px">
                  <Avatar :name="u.name" :email="u.email" small />
                  <div style="min-width: 0">
                    <div class="cell-title">{{ u.email }} <span v-if="u.id === auth.user?.id" class="badge outline">you</span></div>
                    <div v-if="u.name" class="cell-sub">{{ u.name }}</div>
                  </div>
                </div>
              </td>
              <td>
                <select
                  class="select sm"
                  style="width: 120px"
                  :value="u.role"
                  :disabled="!editable(u)"
                  :aria-label="`Role for ${u.email}`"
                  @change="update(u, { role: ($event.target as HTMLSelectElement).value as Role })"
                >
                  <option v-for="r in ROLES" :key="r" :value="r" :disabled="roleRank(r) > roleRank(auth.role)">{{ r }}</option>
                </select>
              </td>
              <td>
                <label class="switch">
                  <input type="checkbox" :checked="u.active" :disabled="!editable(u)" :aria-label="`Active: ${u.email}`" @change="update(u, { active: !u.active })" />
                </label>
              </td>
              <td class="nowrap hide-mobile">{{ relTime(u.last_login_at, now) }}</td>
              <td class="right">
                <button type="button" class="btn btn-sm" :disabled="roleRank(u.role) > roleRank(auth.role)" @click="(resetFor = u), (newPassword = '')"><Icon name="key" />Reset password</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <Modal :open="showNew" title="Add user" :dismissable="!saving" @close="showNew = false">
      <form id="new-user" class="stack" @submit.prevent="create">
        <div class="field">
          <label for="nu-email">Email <span class="req">*</span></label>
          <input id="nu-email" v-model="nf.email" class="input" type="email" required autocomplete="off" />
        </div>
        <div class="field">
          <label for="nu-name">Name</label>
          <input id="nu-name" v-model="nf.name" class="input" />
        </div>
        <div class="field">
          <label for="nu-role">Role</label>
          <select id="nu-role" v-model="nf.role" class="select">
            <option v-for="r in grantable()" :key="r" :value="r">{{ r }}</option>
          </select>
        </div>
        <div class="field">
          <label for="nu-pass">Initial password <span class="req">*</span></label>
          <input id="nu-pass" v-model="nf.password" class="input" type="password" required autocomplete="new-password" />
        </div>
      </form>
      <template #footer>
        <button type="button" class="btn" @click="showNew = false">Cancel</button>
        <button type="submit" form="new-user" class="btn btn-primary" :disabled="saving || !nf.email || !nf.password">Create user</button>
      </template>
    </Modal>

    <Modal :open="!!resetFor" :title="`Reset password · ${resetFor?.email ?? ''}`" :dismissable="!saving" @close="resetFor = null">
      <form id="reset-pass" class="stack" @submit.prevent="resetPassword">
        <div class="field">
          <label for="rp-pass">New password</label>
          <input id="rp-pass" v-model="newPassword" class="input" type="password" required autocomplete="new-password" />
        </div>
      </form>
      <template #footer>
        <button type="button" class="btn" @click="resetFor = null">Cancel</button>
        <button type="submit" form="reset-pass" class="btn btn-primary" :disabled="saving || !newPassword">Reset password</button>
      </template>
    </Modal>
  </div>
</template>
