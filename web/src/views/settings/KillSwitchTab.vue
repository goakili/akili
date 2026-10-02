<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '../../api'
import { useConfirm } from '../../stores/confirm'
import { useLive } from '../../stores/live'
import { useToast } from '../../stores/toast'
import Icon from '../../components/Icon'

const confirm = useConfirm()
const live = useLive()
const toast = useToast()
const busy = ref(false)

async function set(enabled: boolean) {
  const ok = await confirm.ask(
    enabled
      ? {
          title: 'Engage the kill switch?',
          message: 'Every model call and tool call in the organization is refused, and every running session is interrupted. Agents stay connected.',
          confirmText: 'Engage kill switch',
          danger: true,
          requireText: 'STOP',
        }
      : {
          title: 'Release the kill switch?',
          message: 'Agents can call models and tools again. Interrupted sessions continue only on new input; tasks may be retried.',
          confirmText: 'Release kill switch',
        },
  )
  if (!ok) return
  busy.value = true
  try {
    const r = await api.setKillSwitch(enabled)
    live.killSwitch = r.enabled
    toast.success(r.enabled ? 'Kill switch engaged' : 'Kill switch released')
  } catch {
    /* toasted */
  } finally {
    busy.value = false
  }
}

onMounted(() => live.refreshCounts())
</script>

<template>
  <div class="card" style="max-width: 720px">
    <div class="card-body stack loose">
      <div class="row top" style="gap: 16px">
        <div
          style="width: 56px; height: 56px; border-radius: 50%; display: grid; place-items: center; flex: none"
          :style="{ background: live.killSwitch ? 'var(--danger-600)' : 'var(--danger-50)', color: live.killSwitch ? '#fff' : 'var(--danger-text)' }"
          aria-hidden="true"
        >
          <Icon name="power" style="width: 26px; height: 26px" />
        </div>
        <div class="stack tight">
          <h2>Organization kill switch</h2>
          <p class="muted" style="margin: 0">
            An emergency stop for the whole fleet. While engaged, the control plane refuses every model call and every tool
            request, and interrupts running sessions. The action is recorded in the audit log.
          </p>
        </div>
      </div>
      <div class="banner" :class="live.killSwitch ? 'danger' : 'ok'" role="status">
        <Icon :name="live.killSwitch ? 'power' : 'checkCircle'" />
        <div class="banner-body"><strong>{{ live.killSwitch ? 'Engaged.' : 'Released.' }}</strong> {{ live.killSwitch ? 'Agents cannot act.' : 'Agents operate normally within their policies.' }}</div>
      </div>
      <div class="row">
        <button v-if="!live.killSwitch" type="button" class="btn btn-danger btn-lg" :disabled="busy" @click="set(true)">
          <Icon name="power" />Engage kill switch
        </button>
        <button v-else type="button" class="btn btn-lg" :disabled="busy" @click="set(false)"><Icon name="play" />Release kill switch</button>
      </div>
    </div>
  </div>
</template>
