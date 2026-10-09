<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '../../api'
import { useConfirm } from '../../stores/confirm'
import { useToast } from '../../stores/toast'
import Icon from '../../components/Icon'

const confirm = useConfirm()
const toast = useToast()
const root = ref<boolean | null>(null)
const busy = ref(false)
// Re-renders the radios when a change is cancelled: the browser has already moved the checked one.
const rev = ref(0)

async function load() {
  try {
    root.value = (await api.getSandboxSettings()).root
  } catch {
    /* toasted */
  }
}

async function set(next: boolean) {
  if (next === root.value) return
  if (
    next &&
    !(await confirm.ask({
      title: 'Run sandboxes as root?',
      message: 'Commands in project sandboxes (builds, tests, package installs) run as root inside the sandbox container. Capabilities stay dropped and privilege escalation stays blocked. Applies to sessions started from now on.',
      confirmText: 'Run as root',
      danger: true,
    }))
  ) {
    rev.value++
    return
  }
  busy.value = true
  try {
    root.value = (await api.setSandboxSettings({ root: next })).root
    toast.success(next ? 'Sandboxes now run as root' : 'Sandboxes now run as an unprivileged user')
  } catch {
    rev.value++
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="card" style="max-width: 720px">
    <div class="card-body stack loose">
      <div class="stack tight">
        <h2>Sandbox user</h2>
        <p class="muted" style="margin: 0">
          Who commands run as inside a project's sandbox container (<span class="mono">sandbox_exec</span>). Sandboxes always drop every
          Linux capability, block privilege escalation and have CPU, memory and process limits. Changes are recorded in the audit log.
        </p>
      </div>
      <div v-if="root === null" class="skel" style="height: 120px" aria-busy="true" />
      <div v-else :key="rev" class="choices" role="radiogroup" aria-label="Sandbox user" style="grid-template-columns: repeat(auto-fit, minmax(240px, 1fr))">
        <label class="choice" :class="{ on: !root }">
          <input type="radio" name="sbx-user" :checked="!root" :disabled="busy" @change="set(false)" />
          <Icon name="lock" />
          <span><span class="c-title">Unprivileged user (recommended)</span><br /><span class="c-sub">Runs as the agent's user, or uid 10001 when the agent runs as root. Package installs need an image that already has them.</span></span>
        </label>
        <label class="choice" :class="{ on: root }">
          <input type="radio" name="sbx-user" :checked="!!root" :disabled="busy" @change="set(true)" />
          <Icon name="alert" />
          <span><span class="c-title">Root</span><br /><span class="c-sub">Commands can install packages and write anywhere in the sandbox. The workspace still lives on the agent host.</span></span>
        </label>
      </div>
      <div v-if="root" class="banner warn" role="note">
        <Icon name="alert" />
        <div class="banner-body">Sandboxes run as root. A container escape would start as root on the agent host, so use this only on hosts dedicated to the agent.</div>
      </div>
    </div>
  </div>
</template>
