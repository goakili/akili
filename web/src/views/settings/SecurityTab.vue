<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { api, type SecurityStatus } from '../../api'
import { fmtDate, num, relTime } from '../../lib/format'
import { useNow } from '../../lib/now'
import Icon, { type IconName } from '../../components/Icon'
import EmptyState from '../../components/EmptyState.vue'
import SkeletonRows from '../../components/SkeletonRows.vue'

const REFRESH_MS = 10_000

const status = ref<SecurityStatus | null>(null)
const loading = ref(true)
const now = useNow()
let timer: ReturnType<typeof setInterval> | null = null

async function load(background = false) {
  try {
    status.value = await api.security({ quiet: background })
  } catch {
    /* toasted on first load; background refreshes keep the last good state */
  } finally {
    loading.value = false
  }
}

const dataKeys = computed(() => status.value?.data_keys ?? [])
const sinks = computed(() => status.value?.siem ?? [])
const vault = computed(() => status.value?.kms === 'vault-transit')

interface Look {
  tone: '' | 'ok' | 'warn' | 'danger' | 'info'
  icon: IconName
  text: string
  help: string
}

const mtls = computed<Look>(() => {
  switch (status.value?.agent_mtls) {
    case 'required':
      return { tone: 'ok', icon: 'lock', text: 'required', help: 'Agents must present a client certificate issued by the control-plane CA on every connect.' }
    case 'optional':
      return { tone: 'warn', icon: 'alert', text: 'optional', help: 'Agents with a client certificate are verified; agents without one still connect with their keypair alone.' }
    default:
      return { tone: '', icon: 'circle', text: 'off', help: 'Agents authenticate with their Ed25519 keypair and session credentials only; no client certificate is checked.' }
  }
})

function sinkTone(s: { lag: number; last_error?: string }): string {
  if (s.last_error) return 'danger'
  return s.lag > 0 ? 'warn' : 'ok'
}

onMounted(() => {
  load()
  timer = setInterval(() => {
    if (!document.hidden) load(true)
  }, REFRESH_MS)
})
onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>

<template>
  <div class="stack">
    <div v-if="loading && !status" class="card">
      <div class="table-wrap">
        <table class="table"><tbody><SkeletonRows :cols="3" :rows="4" /></tbody></table>
      </div>
    </div>
    <EmptyState v-else-if="!status" title="Security status unavailable" icon="lock">The control plane did not answer. It retries every 10 seconds.</EmptyState>
    <template v-else>
      <div class="card">
        <div class="card-head"><h2><Icon name="lock" />Access</h2></div>
        <div class="card-body">
          <dl class="kv">
            <dt>Single sign-on</dt>
            <dd class="stack tight">
              <span><span class="badge" :class="status.sso ? 'ok' : ''"><Icon :name="status.sso ? 'checkCircle' : 'circle'" />{{ status.sso ? 'on' : 'off' }}</span></span>
              <span class="small muted">{{ status.sso ? 'People can sign in through the identity provider configured on the control plane.' : 'People sign in with email and password. Configure an OIDC provider on the control plane to turn SSO on.' }}</span>
            </dd>
            <dt>TLS</dt>
            <dd class="stack tight">
              <span><span class="badge" :class="status.tls ? 'ok' : 'danger'"><Icon :name="status.tls ? 'checkCircle' : 'alert'" />{{ status.tls ? 'on' : 'off' }}</span></span>
              <span class="small muted">{{ status.tls ? 'The control plane serves HTTPS and WSS itself.' : 'The control plane serves plain HTTP. Only acceptable behind a TLS-terminating proxy or in development.' }}</span>
            </dd>
            <dt>Agent mTLS</dt>
            <dd class="stack tight">
              <span><span class="badge" :class="mtls.tone"><Icon :name="mtls.icon" />{{ mtls.text }}</span></span>
              <span class="small muted">{{ mtls.help }}</span>
            </dd>
          </dl>
        </div>
      </div>

      <div class="card">
        <div class="card-head"><h2><Icon name="key" />Encryption at rest</h2></div>
        <div class="card-body stack">
          <dl class="kv">
            <dt>Key provider</dt>
            <dd class="stack tight">
              <span><span class="badge info"><Icon :name="vault ? 'server' : 'key'" />{{ vault ? 'Vault Transit' : status.kms === 'local' ? 'Local key' : status.kms }}</span></span>
              <span class="small muted">
                <template v-if="vault">Data keys are wrapped by a HashiCorp Vault Transit key; the control plane never holds the wrapping key.</template>
                <template v-else>Data keys are wrapped by <code>AKILI_ENCRYPTION_KEY</code> from the control-plane environment. Store it separately from database backups.</template>
              </span>
            </dd>
          </dl>
          <div class="banner info">
            <Icon name="terminal" />
            <div class="banner-body">
              Rotation is a CLI operation, run on a control-plane host: <code>akili keys rotate</code> creates a new active data key for new
              secrets, and <code>akili keys rewrap</code> re-wraps the stored data keys under the current provider key.
            </div>
          </div>
        </div>
        <div class="table-wrap" style="border-top: 1px solid var(--border-primary)">
          <table class="table">
            <thead><tr><th>Data key</th><th>Provider</th><th>Status</th></tr></thead>
            <tbody>
              <tr v-if="!dataKeys.length"><td colspan="3"><EmptyState title="No data keys yet" icon="key" compact>One is created the first time a secret is stored.</EmptyState></td></tr>
              <tr v-for="k in dataKeys" :key="k.id">
                <td class="mono small">{{ k.id }}</td>
                <td class="small">{{ k.provider }}</td>
                <td>
                  <span v-if="k.active" class="badge ok"><Icon name="checkCircle" />active</span>
                  <span v-else class="badge"><Icon name="circle" />decrypt only</span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <div class="card">
        <div class="card-head">
          <h2><Icon name="activity" />SIEM sinks</h2>
          <span class="small muted">Refreshes every 10 s</span>
        </div>
        <div class="table-wrap">
          <table class="table">
            <thead><tr><th>Sink</th><th class="right">Delivered up to</th><th class="right">Lag</th><th>Last delivery</th><th>Last error</th></tr></thead>
            <tbody>
              <tr v-if="!sinks.length">
                <td colspan="5">
                  <EmptyState title="No SIEM sinks configured" icon="activity" compact>
                    The audit log is not exported. Set <code>AKILI_SIEM_WEBHOOK_URL</code>, <code>AKILI_SIEM_SYSLOG</code> or
                    <code>AKILI_SIEM_FILE</code> on the control plane to stream every audit event to your SIEM.
                  </EmptyState>
                </td>
              </tr>
              <tr v-for="s in sinks" :key="s.sink">
                <td><span class="badge" :class="sinkTone(s)"><Icon :name="s.last_error ? 'xCircle' : s.lag > 0 ? 'hourglass' : 'checkCircle'" />{{ s.sink }}</span></td>
                <td class="right mono small">#{{ s.cursor }}</td>
                <td class="right num">{{ num(s.lag) }}</td>
                <td class="nowrap small" :title="s.last_sent_at ? fmtDate(s.last_sent_at) : undefined">{{ s.last_sent_at ? relTime(s.last_sent_at, now) : 'never' }}</td>
                <td class="small">
                  <span v-if="s.last_error" class="danger-text" style="overflow-wrap: anywhere">{{ s.last_error }}</span>
                  <span v-else class="muted">—</span>
                  <div v-if="s.last_error && s.error_at" class="muted" :title="fmtDate(s.error_at)">{{ relTime(s.error_at, now) }}</div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </template>
  </div>
</template>
