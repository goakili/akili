<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api, type NotificationSettings } from '../../api'
import { useAuth } from '../../stores/auth'
import { useToast } from '../../stores/toast'
import { fmtDate } from '../../lib/format'

const auth = useAuth()
const toast = useToast()
const current = ref('')
const next = ref('')
const again = ref('')
const busy = ref(false)
const error = ref('')

const notif = ref<NotificationSettings | null>(null)
const testing = ref(false)

onMounted(async () => {
  try {
    notif.value = await api.notifications()
  } catch {
    /* toasted */
  }
})

async function setNotif(key: 'email_approvals' | 'email_tasks', ev: Event) {
  const on = (ev.target as HTMLInputElement).checked
  try {
    notif.value = await api.updateNotifications({ [key]: on })
  } catch {
    ;(ev.target as HTMLInputElement).checked = !on
  }
}

async function sendTest() {
  testing.value = true
  try {
    toast.success((await api.testNotification()).message)
  } catch {
    /* toasted */
  } finally {
    testing.value = false
  }
}

async function submit() {
  error.value = ''
  if (next.value !== again.value) {
    error.value = 'The new passwords do not match.'
    return
  }
  busy.value = true
  try {
    await api.changePassword(current.value, next.value)
    toast.success('Password changed')
    current.value = next.value = again.value = ''
  } catch {
    /* toasted */
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="grid-2" style="max-width: 960px; align-items: start">
    <section class="card">
      <div class="card-head"><h2>Profile</h2></div>
      <div class="card-body">
        <dl class="kv">
          <dt>Email</dt><dd>{{ auth.user?.email }}</dd>
          <dt>Name</dt><dd>{{ auth.user?.name || '—' }}</dd>
          <dt>Role</dt><dd>{{ auth.role }}</dd>
          <dt>Organization</dt><dd>{{ auth.me?.organization?.name }}</dd>
          <dt>Signed in via</dt><dd>{{ auth.me?.auth_method || '—' }}</dd>
          <dt>Last login</dt><dd>{{ fmtDate(auth.user?.last_login_at) }}</dd>
        </dl>
      </div>
    </section>
    <section class="card">
      <div class="card-head"><h2>Email notifications</h2></div>
      <div v-if="notif" class="card-body stack">
        <p v-if="!notif.email_available" class="hint" style="margin: 0">
          Email is not set up yet: an admin adds a <strong>Posta</strong> integration under Integrations. Your choices below are kept for when it is.
        </p>
        <label class="switch" :title="notif.can_approve ? undefined : 'Only operators and admins can approve'">
          <input type="checkbox" :checked="notif.email_approvals && notif.can_approve" :disabled="!notif.can_approve" @change="setNotif('email_approvals', $event)" />
          Approval requests
        </label>
        <label class="switch"><input type="checkbox" :checked="notif.email_tasks" @change="setNotif('email_tasks', $event)" />My tasks finish</label>
        <p class="hint" style="margin: 0">
          Sent to {{ auth.user?.email }}. Emails say what happened and link to Akili; tool arguments and output stay in Akili. At most one approval email a minute.
        </p>
        <div class="row" style="justify-content: flex-end">
          <button type="button" class="btn" :disabled="!notif.email_available || testing" @click="sendTest">
            <span v-if="testing" class="spinner" />Send me a test email
          </button>
        </div>
      </div>
    </section>
    <form class="card" @submit.prevent="submit">
      <div class="card-head"><h2>Change password</h2></div>
      <div class="card-body stack">
        <input type="text" autocomplete="username" :value="auth.user?.email" hidden readonly />
        <div class="field">
          <label for="ac-cur">Current password</label>
          <input id="ac-cur" v-model="current" class="input" type="password" autocomplete="current-password" required />
        </div>
        <div class="field">
          <label for="ac-new">New password</label>
          <input id="ac-new" v-model="next" class="input" type="password" autocomplete="new-password" required />
        </div>
        <div class="field">
          <label for="ac-again">Repeat new password</label>
          <input id="ac-again" v-model="again" class="input" type="password" autocomplete="new-password" required />
        </div>
        <div v-if="error" class="form-error" role="alert">{{ error }}</div>
        <div class="row" style="justify-content: flex-end">
          <button type="submit" class="btn btn-primary" :disabled="busy || !current || !next">Change password</button>
        </div>
      </div>
    </form>
  </div>
</template>
