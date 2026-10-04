<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '../api'
import { useAuth } from '../stores/auth'
import Icon from '../components/Icon'

const route = useRoute()
const auth = useAuth()
const q = (k: string) => (typeof route.query[k] === 'string' ? (route.query[k] as string) : '')

const challenge = q('challenge')
const state = q('state')
const client = q('client')
const editorScheme = q('editor')
const windowId = q('window')
const editors: Record<string, string> = { vscode: 'VS Code', 'vscode-insiders': 'VS Code Insiders', vscodium: 'VSCodium', cursor: 'Cursor', windsurf: 'Windsurf' }
const editor = computed(() => (Object.hasOwn(editors, editorScheme) ? editors[editorScheme] : ''))
const complete = computed(() => !!(challenge && state && client && editor.value))

const busy = ref(false)
const done = ref(false)
const error = ref('')

async function approve() {
  busy.value = true
  error.value = ''
  try {
    const r = await api.vscodeAuthorize({ challenge, state, client, editor: editorScheme, window: windowId })
    done.value = true
    window.location.href = r.redirect
  } catch (e) {
    error.value = (e as Error).message
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
      </div>
      <section class="card login-form stack" aria-labelledby="vsc-title">
        <template v-if="!complete">
          <h2 id="vsc-title" style="font-size: 17px">This sign-in link is incomplete</h2>
          <p class="small muted" style="margin: 0">Start the sign-in again from your editor: run <strong>Akili: Sign in</strong>.</p>
        </template>
        <template v-else-if="done">
          <h2 id="vsc-title" style="font-size: 17px">Return to {{ editor }}</h2>
          <p class="small muted" style="margin: 0">If {{ editor }} did not open, allow the browser to open it, or start the sign-in again.</p>
        </template>
        <template v-else>
          <div>
            <h2 id="vsc-title" style="font-size: 17px">Sign in to {{ editor }}?</h2>
            <p class="small muted" style="margin: 4px 0 0"><strong>{{ client }}</strong> asks to use Akili as you.</p>
          </div>
          <ul class="small" style="margin: 0; padding-left: 18px">
            <li>It acts as <strong>{{ auth.user?.email }}</strong> with your role ({{ auth.role }}), for 90 days.</li>
            <li>It can chat with agents, start tasks and decide approvals you are allowed to decide.</li>
            <li>You can revoke it any time under <strong>Settings → API keys</strong>.</li>
          </ul>
          <div class="banner warn" role="note">
            <Icon name="alert" />
            <div class="banner-body">Only approve if you just started this sign-in from your own editor.</div>
          </div>
          <div v-if="error" class="form-error" role="alert"><Icon name="alert" />{{ error }}</div>
          <div class="row" style="justify-content: flex-end; gap: 8px">
            <RouterLink to="/" class="btn">Cancel</RouterLink>
            <button type="button" class="btn btn-primary" :disabled="busy" @click="approve">
              <span v-if="busy" class="spinner" aria-hidden="true" />Approve and open {{ editor }}
            </button>
          </div>
        </template>
      </section>
    </div>
  </main>
</template>
