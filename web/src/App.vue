<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useAuth } from './stores/auth'
import { useLive } from './stores/live'
import { useSound } from './stores/sound'
import { useUi } from './stores/ui'
import { useConfirm } from './stores/confirm'
import { useToast } from './stores/toast'
import { api } from './api'
import Icon from './components/Icon'
import ToastHost from './components/ToastHost.vue'
import ConfirmHost from './components/ConfirmHost.vue'
import AppSidebar from './components/shell/AppSidebar.vue'
import ApprovalsBell from './components/shell/ApprovalsBell.vue'
import SoundToggle from './components/shell/SoundToggle.vue'
import ThemeToggle from './components/shell/ThemeToggle.vue'
import UserMenu from './components/shell/UserMenu.vue'

const auth = useAuth()
const live = useLive()
const sound = useSound()
const ui = useUi()
const confirm = useConfirm()
const toast = useToast()
const route = useRoute()

const bare = computed(() => route.meta.public === true || route.meta.bare === true)

// The org event feed runs while someone is signed in.
watch(
  () => auth.user?.id,
  (id) => {
    if (id) {
      live.start(() => void api.me({ quiet: true }).catch(() => {}))
      sound.start()
    } else {
      live.stop()
      sound.stop()
    }
  },
  { immediate: true },
)

watch(
  () => route.fullPath,
  () => (ui.drawerOpen = false),
)

// "(2) Page · Akili": detail pages publish their object's name through ui.crumb, and the count of
// approvals waiting shows in background tabs too.
const pageTitle = computed(() => ui.crumb || route.meta.title || '')
const waiting = computed(() => (auth.isOperator && live.pendingApprovals > 0 ? `(${live.pendingApprovals}) ` : ''))
watch(
  [pageTitle, waiting],
  ([t, w]) => {
    document.title = w + (t ? `${t} · Akili` : 'Akili')
  },
  { immediate: true },
)

const viewKey = computed(() => `${String(route.name ?? '')}:${String(route.params.id ?? '')}`)

const releasing = ref(false)
async function releaseKill() {
  const ok = await confirm.ask({
    title: 'Release the kill switch?',
    message: 'Agents can call models and tools again. Interrupted sessions continue only on new input; tasks may be retried.',
    confirmText: 'Release kill switch',
  })
  if (!ok) return
  releasing.value = true
  try {
    const r = await api.setKillSwitch(false)
    live.killSwitch = r.enabled
    toast.success('Kill switch released')
  } catch {
    /* toasted */
  } finally {
    releasing.value = false
  }
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape' && ui.drawerOpen) ui.drawerOpen = false
}
</script>

<template>
  <RouterView v-if="bare" />
  <div v-else class="shell" :class="{ collapsed: ui.collapsed }" @keydown="onKey">
    <div class="scrim" :class="{ open: ui.drawerOpen }" aria-hidden="true" @click="ui.drawerOpen = false" />
    <AppSidebar :version="live.version" />

    <div class="main" :class="{ 'has-banner': live.killSwitch }">
      <div class="app-header">
        <header class="topbar">
          <button type="button" class="icon-btn menu-btn" aria-label="Open navigation" :aria-expanded="ui.drawerOpen" @click="ui.drawerOpen = !ui.drawerOpen">
            <Icon name="menu" />
          </button>
          <nav class="topbar-crumbs" aria-label="Breadcrumb">
            <template v-if="route.meta.parent">
              <RouterLink :to="route.meta.parent.to" class="hide-mobile">{{ route.meta.parent.label }}</RouterLink>
              <Icon name="chevronRight" class="hide-mobile" />
            </template>
            <span class="current truncate" aria-current="page">{{ pageTitle }}</span>
          </nav>
          <div class="topbar-actions">
            <ApprovalsBell />
            <SoundToggle />
            <ThemeToggle />
            <UserMenu />
          </div>
        </header>
        <div v-if="live.killSwitch" class="kill-banner" role="alert">
          <Icon name="power" />
          <span class="grow"><strong>Kill switch engaged.</strong> <span class="hide-mobile">Every model call and tool call in the organization is refused.</span></span>
          <button v-if="auth.isAdmin" type="button" class="btn btn-sm" :disabled="releasing" @click="releaseKill">
            <span v-if="releasing" class="spinner" />Release
          </button>
        </div>
      </div>
      <main id="main" class="content" :class="{ fill: route.meta.fill }">
        <RouterView :key="viewKey" />
      </main>
      <footer v-if="!route.meta.fill" class="app-footer">
        <span>Akili · Copyright © 2026 <a href="https://jkaninda.dev/" target="_blank" rel="noopener noreferrer">Jonas Kaninda</a></span>
        <RouterLink to="/about">About</RouterLink>
      </footer>
    </div>
  </div>
  <ToastHost />
  <ConfirmHost />
</template>
