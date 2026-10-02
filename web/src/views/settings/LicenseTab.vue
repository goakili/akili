<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api, ApiError, type LicenseInfo } from '../../api'
import { fmtDate } from '../../lib/format'
import { useAuth } from '../../stores/auth'
import { useConfirm } from '../../stores/confirm'
import { useToast } from '../../stores/toast'
import Icon, { type IconName } from '../../components/Icon'
import CopyField from '../../components/CopyField.vue'

const auth = useAuth()
const confirm = useConfirm()
const toast = useToast()
const info = ref<LicenseInfo | null>(null)
const token = ref('')
const busy = ref(false)
const error = ref('')

const isOwner = computed(() => auth.can('owner'))
const enterprise = computed(() => info.value?.edition === 'enterprise')
const agentLimit = computed(() => info.value?.limits?.agents)

const status = computed((): { tone: string; icon: IconName; text: string } | null => {
  const l = info.value
  if (!l) return null
  switch (l.state) {
    case 'valid':
      return { tone: 'ok', icon: 'checkCircle', text: `Licensed to ${l.customer} until ${fmtDate(l.not_after)}.` }
    case 'grace':
      return { tone: 'warn', icon: 'clock', text: `Expired on ${fmtDate(l.not_after)}. Everything keeps working until ${fmtDate(l.grace_ends)}; renew to keep changing Enterprise settings after that.` }
    case 'degraded':
      return { tone: 'warn', icon: 'alert', text: `Expired on ${fmtDate(l.not_after)}. Enterprise features keep running, but their settings are read-only until the license is renewed.` }
    case 'binding_mismatch':
      return { tone: 'danger', icon: 'xCircle', text: `This license was issued for another deployment (${l.binding_error}). No Enterprise feature is enabled.` }
    default:
      return l.licensable
        ? { tone: 'info', icon: 'info', text: 'Community edition. Install an Akili Enterprise license to enable the features below.' }
        : { tone: 'info', icon: 'info', text: 'Community edition. This build cannot activate a license: it is a Community build or has no license public key.' }
  }
})

async function load() {
  try {
    info.value = await api.getLicense({ quiet: true })
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : 'Could not load the license.'
  }
}

async function install() {
  if (!token.value.trim()) return
  busy.value = true
  error.value = ''
  try {
    info.value = await api.installLicense(token.value.trim())
    token.value = ''
    toast.success(`License installed for ${info.value.customer}`)
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : 'The license was not installed.'
  } finally {
    busy.value = false
  }
}

async function remove() {
  const ok = await confirm.ask({
    title: 'Remove the license?',
    message: 'The control plane continues as the Community edition. Enterprise features stop. Policies, approvals, audit and agents are not affected.',
    confirmText: 'Remove license',
    danger: true,
  })
  if (!ok) return
  busy.value = true
  try {
    info.value = await api.removeLicense()
    toast.success('License removed')
  } catch {
    /* toasted */
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="stack loose" style="max-width: 880px">
    <section class="card">
      <div class="card-head">
        <h2><Icon name="award" />Edition</h2>
        <span v-if="info" class="badge" :class="enterprise ? 'accent' : ''">{{ enterprise ? 'Enterprise' : 'Community' }}</span>
      </div>
      <div class="card-body stack">
        <div v-if="status" class="banner" :class="status.tone" role="status">
          <Icon :name="status.icon" />
          <div class="banner-body">{{ status.text }}</div>
        </div>
        <div v-if="info?.install_id" class="stack tight">
          <CopyField :value="info.install_id" label="Install ID" />
          <span class="small muted">Identifies this deployment. Quote it when you request a license: the license is bound to it.</span>
        </div>
        <dl v-if="info && info.state !== 'none'" class="kv">
          <dt>Customer</dt><dd>{{ info.customer || '—' }}</dd>
          <dt>License</dt><dd class="mono small">{{ info.license_id || '—' }}</dd>
          <dt>Expires</dt><dd>{{ fmtDate(info.not_after) }}</dd>
          <dt>Grace ends</dt><dd>{{ fmtDate(info.grace_ends) }}</dd>
          <template v-if="info.license_install_id"><dt>Bound to install</dt><dd class="mono small">{{ info.license_install_id }}</dd></template>
          <template v-if="info.url"><dt>Bound to URL</dt><dd class="mono small">{{ info.url }}</dd></template>
          <dt>Agents</dt>
          <dd>{{ info.agents_in_use }} in use<template v-if="agentLimit !== undefined"> · {{ agentLimit < 0 ? 'unlimited' : `${agentLimit} licensed` }}</template></dd>
        </dl>
        <p class="small muted" style="margin: 0">
          Policies, approvals, the audit log, SSO (OIDC), SIEM streaming and the kill switch are part of every edition. A
          lapsed license never switches a feature off: it only freezes its settings.
        </p>
      </div>
    </section>

    <section v-if="info" class="card">
      <div class="card-head"><h2><Icon name="sparkles" />Enterprise features</h2></div>
      <div class="card-body">
        <ul class="license-features">
          <li v-for="f in info.features" :key="f.name" :class="{ granted: f.granted }">
            <Icon :name="f.granted ? 'checkCircle' : 'circle'" />
            <span>{{ f.description }}</span>
          </li>
        </ul>
      </div>
    </section>

    <section v-if="info?.licensable && isOwner" class="card">
      <div class="card-head"><h2><Icon name="key" />{{ enterprise ? 'Replace the license' : 'Install a license' }}</h2></div>
      <form class="card-body stack" @submit.prevent="install">
        <label for="license-token" class="small muted">Paste the license token (it starts with <span class="mono">akili-v1.</span>). It is verified offline.</label>
        <textarea id="license-token" v-model="token" class="textarea mono" rows="4" spellcheck="false" placeholder="akili-v1.…" />
        <div v-if="error" class="banner danger" role="alert"><Icon name="alert" /><div class="banner-body">{{ error }}</div></div>
        <div class="row">
          <button type="submit" class="btn btn-primary" :disabled="busy || !token.trim()"><span v-if="busy" class="spinner" />Install license</button>
          <button v-if="enterprise" type="button" class="btn btn-danger" :disabled="busy" @click="remove">Remove license</button>
        </div>
      </form>
    </section>
    <p v-else-if="info?.licensable" class="small muted">Only the owner can install or remove the license.</p>
  </div>
</template>

<style scoped>
.license-features {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 8px 20px;
}
.license-features li {
  display: flex;
  gap: 8px;
  align-items: flex-start;
  font-size: 13.5px;
  color: var(--text-tertiary);
}
.license-features li.granted {
  color: var(--text-primary);
}
.license-features li .icon {
  flex: none;
  width: 16px;
  height: 16px;
  margin-top: 2px;
}
.license-features li.granted .icon {
  color: var(--success-500);
}
</style>
