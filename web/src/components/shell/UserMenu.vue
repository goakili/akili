<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuth } from '../../stores/auth'
import { useLive } from '../../stores/live'
import { menuKeys, usePopover } from '../../lib/popover'
import Icon from '../Icon'
import Avatar from '../Avatar.vue'

const auth = useAuth()
const live = useLive()
const router = useRouter()
const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLElement | null>(null)
const { open, toggle, close } = usePopover(root, trigger)

function account() {
  close()
  router.push({ path: '/settings', query: { tab: 'account' } })
}

async function logout() {
  close()
  await auth.logout().catch(() => {})
  live.stop()
  router.replace({ name: 'login' })
}
</script>

<template>
  <div ref="root" class="popover-anchor">
    <button ref="trigger" type="button" class="user-btn" aria-haspopup="menu" :aria-expanded="open" :aria-label="`Account menu for ${auth.user?.email ?? ''}`" @click="toggle">
      <Avatar :name="auth.user?.name" :email="auth.user?.email" />
      <Icon name="chevronDown" class="hide-mobile" />
    </button>
    <div v-if="open" class="menu" role="menu" aria-label="Account" style="min-width: 250px" @keydown="menuKeys">
      <div class="menu-head row" style="gap: 10px">
        <Avatar :name="auth.user?.name" :email="auth.user?.email" />
        <div class="grow">
          <div class="strong truncate">{{ auth.user?.name || auth.user?.email?.split('@')[0] }}</div>
          <div class="small muted truncate">{{ auth.user?.email }}</div>
        </div>
      </div>
      <div style="padding: 0 10px 8px" class="row">
        <span class="badge accent" style="text-transform: capitalize">{{ auth.role }}</span>
        <span v-if="auth.me?.organization?.name" class="small muted truncate">{{ auth.me.organization.name }}</span>
      </div>
      <div class="menu-sep" />
      <button type="button" role="menuitem" class="menu-item" @click="account"><Icon name="user" />Account</button>
      <button type="button" role="menuitem" class="menu-item danger" @click="logout"><Icon name="logout" />Log out</button>
    </div>
  </div>
</template>
