<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuth } from '../stores/auth'
import { api, ApiError, type AuthProviders } from '../api'
import { safeNavUrl } from '../lib/format'
import Icon from '../components/Icon'

const auth = useAuth()
const router = useRouter()
const route = useRoute()
const email = ref('')
const password = ref('')
const error = ref('')
const busy = ref(false)
const show = ref(false)
const providers = ref<AuthProviders | null>(null)
const redirecting = ref(false)
const ssoError = ref(typeof route.query.sso_error === 'string' ? route.query.sso_error.slice(0, 500) : '')

const ssoHref = computed(() => (providers.value?.sso ? safeNavUrl(providers.value.sso_login_url) : undefined))
const ssoName = computed(() => providers.value?.sso_name?.trim() || 'SSO')
// Fall back to the password form when the providers call fails, so sign-in never disappears.
const passwordEnabled = computed(() => providers.value?.password ?? true)

// A full-page navigation, not fetch: the server answers with a redirect to the identity provider.
function ssoSignIn() {
  if (!ssoHref.value) return
  redirecting.value = true
  window.location.href = ssoHref.value
}

onMounted(async () => {
  if (route.query.sso_error !== undefined) {
    const query = { ...route.query }
    delete query.sso_error
    router.replace({ query })
  }
  try {
    providers.value = await api.authProviders()
  } catch {
    providers.value = null
  }
})

async function submit() {
  error.value = ''
  busy.value = true
  try {
    await auth.login(email.value.trim(), password.value)
    const next = typeof route.query.next === 'string' && route.query.next.startsWith('/') && !route.query.next.startsWith('//') ? route.query.next : '/'
    router.replace(next)
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : 'Sign-in failed.'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main class="login-wrap">
    <div class="login-card">
      <div class="login-brand">
        <img src="/logo-icon.svg" alt="" width="72" height="72" />
        <h1>akili</h1>
        <p>Security-first control plane for autonomous agents</p>
      </div>
      <form class="card login-form stack" aria-labelledby="signin-title" @submit.prevent="submit">
        <div>
          <h2 id="signin-title" style="font-size: 17px">Sign in</h2>
          <p class="small muted" style="margin: 4px 0 0">{{ ssoHref ? `Use ${ssoName}, or your operator account.` : 'Use your operator account.' }}</p>
        </div>
        <div v-if="ssoError" class="banner danger" role="alert">
          <Icon name="alert" />
          <div class="banner-body"><strong>Single sign-on failed.</strong> {{ ssoError }}</div>
        </div>
        <template v-if="ssoHref">
          <button type="button" class="btn btn-primary btn-lg btn-block" :disabled="redirecting" @click="ssoSignIn">
            <span v-if="redirecting" class="spinner" aria-hidden="true" /><Icon v-else name="key" />{{ redirecting ? 'Redirecting…' : `Sign in with ${ssoName}` }}
          </button>
          <div v-if="passwordEnabled" class="login-or" role="separator"><span>or</span></div>
        </template>
        <template v-if="passwordEnabled">
          <div class="field">
            <label for="email">Email</label>
            <input id="email" v-model="email" class="input" type="email" autocomplete="username" required :autofocus="!ssoHref" placeholder="you@company.com" />
          </div>
          <div class="field">
            <label for="password">Password</label>
            <div class="search-input">
              <input id="password" v-model="password" class="input" :type="show ? 'text' : 'password'" autocomplete="current-password" required style="padding-right: 44px" />
              <button
                type="button"
                class="btn btn-ghost btn-icon btn-sm"
                style="position: absolute; right: 4px"
                :aria-label="show ? 'Hide password' : 'Show password'"
                :aria-pressed="show"
                @click="show = !show"
              >
                <Icon :name="show ? 'lock' : 'eye'" />
              </button>
            </div>
          </div>
          <div v-if="error" class="form-error" role="alert"><Icon name="alert" />{{ error }}</div>
          <button class="btn btn-lg btn-block" :class="ssoHref ? '' : 'btn-primary'" type="submit" :disabled="busy || !email || !password">
            <span v-if="busy" class="spinner" aria-hidden="true" />{{ busy ? 'Signing in…' : ssoHref ? 'Sign in with password' : 'Sign in' }}
          </button>
          <p v-if="providers?.password_note" class="small muted" style="margin: 0; text-align: center">{{ providers.password_note }}</p>
        </template>
      </form>
      <p class="login-foot">Every agent action is policy-checked and recorded in a tamper-evident audit log.</p>
    </div>
  </main>
</template>
