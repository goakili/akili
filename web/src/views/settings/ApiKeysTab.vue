<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api, type APIKey, type APIKeyCreated } from '../../api'
import { useAuth } from '../../stores/auth'
import { useConfirm } from '../../stores/confirm'
import { useToast } from '../../stores/toast'
import { fmtDate, relTime } from '../../lib/format'
import { useNow } from '../../lib/now'
import Modal from '../../components/Modal.vue'
import CopyField from '../../components/CopyField.vue'
import Badge from '../../components/Badge.vue'
import Icon from '../../components/Icon'
import EmptyState from '../../components/EmptyState.vue'
import SkeletonRows from '../../components/SkeletonRows.vue'

const auth = useAuth()
const confirm = useConfirm()
const toast = useToast()
const now = useNow()
const keys = ref<APIKey[]>([])
const loading = ref(true)
const show = ref(false)
const name = ref('')
const scopes = ref<string[]>(['read'])
const days = ref(90)
const created = ref<APIKeyCreated | null>(null)
const saving = ref(false)

async function load() {
  try {
    keys.value = (await api.listAPIKeys()) ?? []
  } catch {
    /* toasted */
  } finally {
    loading.value = false
  }
}

function openNew() {
  name.value = ''
  scopes.value = ['read']
  days.value = 90
  created.value = null
  show.value = true
}

async function create() {
  saving.value = true
  try {
    created.value = await api.createAPIKey({ name: name.value.trim(), scopes: scopes.value, expires_in_days: Number(days.value) || 0 })
    load()
  } catch {
    /* toasted */
  } finally {
    saving.value = false
  }
}

async function revoke(k: APIKey) {
  if (!(await confirm.ask({ title: `Revoke key ${k.name}?`, message: `Clients using ${k.prefix}… stop working immediately. This cannot be undone.`, confirmText: `Revoke ${k.name}`, danger: true }))) return
  try {
    await api.revokeAPIKey(k.id)
    toast.success(`Key ${k.name} revoked`)
    load()
  } catch {
    /* toasted */
  }
}

function toggleScope(s: string) {
  scopes.value = scopes.value.includes(s) ? scopes.value.filter((x) => x !== s) : [...scopes.value, s]
}

onMounted(load)
</script>

<template>
  <div class="stack">
    <div class="row end">
      <button type="button" class="btn btn-primary" @click="openNew"><Icon name="plus" />Create key</button>
    </div>
    <div class="card">
      <div class="table-wrap">
        <table class="table">
          <thead><tr><th>Name</th><th>Prefix</th><th>Scopes</th><th class="hide-mobile">Last used</th><th class="hide-mobile">Expires</th><th>Status</th><th><span class="sr-only">Actions</span></th></tr></thead>
          <tbody>
            <SkeletonRows v-if="loading" :cols="7" :rows="2" />
            <tr v-else-if="!keys.length">
              <td colspan="7">
                <EmptyState title="No API keys" icon="key">
                  Personal keys for scripts and CI (<code>Authorization: Bearer ak_…</code>). They act as you, capped by their scopes.
                  <template #actions><button type="button" class="btn btn-primary" @click="openNew"><Icon name="plus" />Create key</button></template>
                </EmptyState>
              </td>
            </tr>
            <tr v-for="k in keys" :key="k.id">
              <td class="cell-title">{{ k.name }}</td>
              <td class="mono small">{{ k.prefix }}…</td>
              <td><span v-for="s in k.scopes ?? []" :key="s" class="label-chip">{{ s }}</span></td>
              <td class="nowrap hide-mobile">{{ relTime(k.last_used_at, now) }}</td>
              <td class="nowrap hide-mobile">{{ k.expires_at ? fmtDate(k.expires_at) : 'never' }}</td>
              <td>
                <Badge v-if="k.revoked_at" value="revoked" />
                <Badge v-else-if="k.expires_at && new Date(k.expires_at).getTime() < now" value="expired" />
                <Badge v-else value="active" />
              </td>
              <td class="right">
                <button v-if="!k.revoked_at" type="button" class="btn btn-sm btn-danger-ghost" @click="revoke(k)">Revoke</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <Modal :open="show" :title="created ? 'Your new API key' : 'Create API key'" :dismissable="!saving" @close="show = false">
      <div v-if="created" class="stack">
        <div class="banner warn" role="alert"><Icon name="key" /><div class="banner-body"><strong>Shown once.</strong> Store this secret now; it cannot be displayed again.</div></div>
        <CopyField label="Secret" :value="created.secret" />
      </div>
      <form v-else id="new-key" class="stack" @submit.prevent="create">
        <div class="field">
          <label for="ak-name">Name <span class="req">*</span></label>
          <input id="ak-name" v-model="name" class="input" required placeholder="ci-deploy" />
        </div>
        <div class="field">
          <span id="ak-scopes" class="label">Scopes</span>
          <div class="chips" role="group" aria-labelledby="ak-scopes">
            <label v-for="s in ['read', 'write']" :key="s" class="chip-toggle" :class="{ on: scopes.includes(s) }">
              <input type="checkbox" :checked="scopes.includes(s)" @change="toggleScope(s)" />{{ s }}
            </label>
          </div>
          <span class="hint">write also needs your role ({{ auth.role }}) to allow the action.</span>
        </div>
        <div class="field">
          <label for="ak-days">Expires in (days)</label>
          <input id="ak-days" v-model.number="days" class="input" type="number" min="0" />
          <span class="hint">0 = never expires.</span>
        </div>
      </form>
      <template #footer>
        <template v-if="created"><button type="button" class="btn btn-primary" @click="show = false">Done</button></template>
        <template v-else>
          <button type="button" class="btn" @click="show = false">Cancel</button>
          <button type="submit" form="new-key" class="btn btn-primary" :disabled="saving || !name.trim() || !scopes.length">Create key</button>
        </template>
      </template>
    </Modal>
  </div>
</template>
