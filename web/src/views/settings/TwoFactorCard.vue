<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api, ApiError, type TOTPSetup, type TwoFactorStatus } from '../../api'
import { useAuth } from '../../stores/auth'
import { useConfirm } from '../../stores/confirm'
import { useToast } from '../../stores/toast'
import CopyField from '../../components/CopyField.vue'
import Icon from '../../components/Icon'

const auth = useAuth()
const confirm = useConfirm()
const toast = useToast()
const status = ref<TwoFactorStatus | null>(null)
const step = ref<'idle' | 'password' | 'scan' | 'codes'>('idle')
const password = ref('')
const setup = ref<TOTPSetup | null>(null)
const code = ref('')
const recovery = ref<string[]>([])
const busy = ref(false)
const error = ref('')

async function load() {
  try {
    status.value = await api.twoFactor()
  } catch {
    /* toasted */
  }
}

onMounted(load)

async function run(fn: () => Promise<void>) {
  error.value = ''
  busy.value = true
  try {
    await fn()
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : 'Something went wrong.'
  } finally {
    busy.value = false
  }
}

function reset() {
  step.value = 'idle'
  password.value = code.value = error.value = ''
  setup.value = null
}

const begin = () =>
  run(async () => {
    setup.value = await api.setupTwoFactor(password.value)
    password.value = ''
    step.value = 'scan'
  })

const enable = () =>
  run(async () => {
    recovery.value = (await api.enableTwoFactor(code.value.trim())).recovery_codes
    code.value = ''
    setup.value = null
    step.value = 'codes'
    await Promise.all([load(), auth.load()])
  })

const regenerate = () =>
  run(async () => {
    recovery.value = (await api.regenerateRecoveryCodes(code.value.trim())).recovery_codes
    code.value = ''
    step.value = 'codes'
    await load()
  })

async function disable() {
  const ok = await confirm.ask({
    title: 'Turn off two-factor authentication?',
    message: 'Signing in will need only your password. Your recovery codes stop working.',
    confirmText: 'Turn off',
    danger: true,
  })
  if (!ok) return
  await run(async () => {
    await api.disableTwoFactor(code.value.trim())
    toast.success('Two-factor authentication turned off')
    reset()
    await Promise.all([load(), auth.load()])
  })
}
</script>

<template>
  <section class="card">
    <div class="card-head">
      <h2>Two-factor authentication</h2>
      <span v-if="status" class="badge" :class="status.enabled ? 'ok' : 'warn'"><Icon :name="status.enabled ? 'lock' : 'alert'" />{{ status.enabled ? 'on' : 'off' }}</span>
    </div>
    <div v-if="status" class="card-body stack">
      <template v-if="step === 'codes'">
        <p class="hint" style="margin: 0">
          Save these recovery codes somewhere safe. Each one signs you in once if you lose your authenticator. They are shown only now.
        </p>
        <CopyField label="Recovery codes" :value="recovery.join('\n')" />
        <div class="row" style="justify-content: flex-end">
          <button type="button" class="btn btn-primary" @click="(recovery = []), reset()">I saved them</button>
        </div>
      </template>

      <template v-else-if="!status.enabled && step === 'idle'">
        <p class="hint" style="margin: 0">Ask for a code from an authenticator app (1Password, Google Authenticator, Aegis…) after your password at sign-in.</p>
        <div class="row" style="justify-content: flex-end">
          <button type="button" class="btn btn-primary" @click="step = 'password'"><Icon name="key" />Set up</button>
        </div>
      </template>

      <form v-else-if="step === 'password'" class="stack" @submit.prevent="begin">
        <input type="text" autocomplete="username" :value="auth.user?.email" hidden readonly />
        <div class="field">
          <label for="tf-pass">Confirm your password</label>
          <input id="tf-pass" v-model="password" class="input" type="password" autocomplete="current-password" required autofocus />
        </div>
        <div v-if="error" class="form-error" role="alert"><Icon name="alert" />{{ error }}</div>
        <div class="row" style="justify-content: flex-end">
          <button type="button" class="btn" @click="reset">Cancel</button>
          <button type="submit" class="btn btn-primary" :disabled="busy || !password"><span v-if="busy" class="spinner" />Continue</button>
        </div>
      </form>

      <form v-else-if="step === 'scan' && setup" class="stack" @submit.prevent="enable">
        <p class="hint" style="margin: 0">Scan this code with your authenticator app, or enter the key by hand, then type the 6-digit code it shows.</p>
        <img :src="setup.qr_code" alt="QR code for your authenticator app" width="200" height="200" style="align-self: center; image-rendering: pixelated; background: #fff; padding: 8px; border-radius: 8px" />
        <CopyField label="Setup key" :value="setup.secret" />
        <div class="field">
          <label for="tf-code">Authentication code</label>
          <input id="tf-code" v-model="code" class="input mono" inputmode="numeric" autocomplete="one-time-code" maxlength="7" placeholder="123456" required />
        </div>
        <div v-if="error" class="form-error" role="alert"><Icon name="alert" />{{ error }}</div>
        <div class="row" style="justify-content: flex-end">
          <button type="button" class="btn" @click="reset">Cancel</button>
          <button type="submit" class="btn btn-primary" :disabled="busy || !code.trim()"><span v-if="busy" class="spinner" />Turn on</button>
        </div>
      </form>

      <form v-else-if="status.enabled" class="stack" @submit.prevent>
        <p class="hint" style="margin: 0">
          Password sign-in asks for a code from your authenticator app. {{ status.recovery_codes_left }} recovery code{{ status.recovery_codes_left === 1 ? '' : 's' }} left.
        </p>
        <div class="field">
          <label for="tf-manage">Authentication code</label>
          <input id="tf-manage" v-model="code" class="input mono" autocomplete="one-time-code" maxlength="16" placeholder="123456" aria-describedby="tf-manage-hint" />
          <span id="tf-manage-hint" class="hint">Needed to change these settings. To turn it off you can also use a recovery code.</span>
        </div>
        <div v-if="error" class="form-error" role="alert"><Icon name="alert" />{{ error }}</div>
        <div class="row" style="justify-content: flex-end">
          <button type="button" class="btn" :disabled="busy || !code.trim()" @click="regenerate"><Icon name="refresh" />New recovery codes</button>
          <button type="button" class="btn btn-danger" :disabled="busy || !code.trim()" @click="disable">Turn off</button>
        </div>
      </form>
    </div>
  </section>
</template>
