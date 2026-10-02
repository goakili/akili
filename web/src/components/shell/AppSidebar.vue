<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import type { Role } from '../../api'
import { useAuth } from '../../stores/auth'
import { useLive } from '../../stores/live'
import { useUi } from '../../stores/ui'
import Icon, { type IconName } from '../Icon'

defineProps<{ version?: string }>()
const auth = useAuth()
const live = useLive()
const ui = useUi()
const route = useRoute()

interface Item {
  to: string
  label: string
  icon: IconName
  badge?: 'approvals'
  minRole?: Role
  /** For /settings?tab=… entries: the tabs that make this item active. */
  tabs?: string[]
}

const SECTIONS: { label: string; items: Item[] }[] = [
  { label: 'Overview', items: [{ to: '/', label: 'Dashboard', icon: 'overview' }] },
  {
    label: 'Operate',
    items: [
      { to: '/agents', label: 'Agents', icon: 'agents' },
      { to: '/projects', label: 'Projects', icon: 'repo' },
      { to: '/sessions', label: 'Chat & sessions', icon: 'chat' },
      { to: '/tasks', label: 'Tasks', icon: 'tasks' },
      { to: '/schedules', label: 'Schedules', icon: 'schedules' },
      { to: '/approvals', label: 'Approvals', icon: 'approvals', badge: 'approvals' },
      { to: '/changes', label: 'Changes', icon: 'clipboard' },
    ],
  },
  {
    label: 'Govern',
    items: [
      { to: '/policies', label: 'Policies', icon: 'policies' },
      { to: '/skills', label: 'Skills', icon: 'skills' },
      { to: '/lessons', label: 'Lessons', icon: 'lightbulb' },
      { to: '/audit', label: 'Audit log', icon: 'scroll', minRole: 'admin' },
      { to: '/settings?tab=security', label: 'Security', icon: 'lock', minRole: 'admin', tabs: ['security'] },
    ],
  },
  {
    label: 'System',
    items: [
      { to: '/settings?tab=providers', label: 'Model providers', icon: 'cpu', minRole: 'admin', tabs: ['providers'] },
      { to: '/integrations', label: 'Integrations', icon: 'plug', minRole: 'admin' },
      { to: '/mcp', label: 'MCP servers', icon: 'network', minRole: 'admin' },
      { to: '/alerts', label: 'Alerts', icon: 'siren', minRole: 'admin' },
      { to: '/miabi', label: 'Miabi', icon: 'layers' },
      { to: '/chat', label: 'Chat', icon: 'messages' },
      { to: '/settings?tab=users', label: 'Users & keys', icon: 'users', tabs: ['users', 'keys'] },
      { to: '/settings', label: 'Settings', icon: 'settings', tabs: [] },
      { to: '/about', label: 'About', icon: 'info' },
    ],
  },
]

const sections = computed(() =>
  SECTIONS.map((s) => ({ ...s, items: s.items.filter((i) => !i.minRole || auth.can(i.minRole)) })).filter((s) => s.items.length),
)

// Users & keys: admins land on users, everyone else on their own API keys.
function target(i: Item): string {
  if (i.tabs?.includes('users') && !auth.isAdmin) return '/settings?tab=keys'
  return i.to
}

function isActive(i: Item): boolean {
  if (i.tabs) {
    if (route.path !== '/settings') return false
    const tab = String(route.query.tab ?? '')
    const claimed = SECTIONS.flatMap((s) => s.items).flatMap((x) => x.tabs ?? [])
    return i.tabs.length ? i.tabs.includes(tab) : !claimed.includes(tab)
  }
  return i.to === '/' ? route.path === '/' : route.path === i.to || route.path.startsWith(i.to + '/')
}

const streamLabel = computed(() =>
  live.status === 'open' ? 'Live' : live.status === 'closed' ? 'Offline' : 'Reconnecting…',
)
</script>

<template>
  <aside class="sidebar" :class="{ open: ui.drawerOpen }" aria-label="Sidebar">
    <RouterLink to="/" class="sb-brand" aria-label="Akili home">
      <img src="/logo-mark-light.svg" alt="" width="32" height="32" />
      <span class="wordmark">akili</span>
    </RouterLink>
    <nav class="sb-nav" aria-label="Main">
      <template v-for="s in sections" :key="s.label">
        <div class="sb-label">{{ s.label }}</div>
        <RouterLink
          v-for="i in s.items"
          :key="i.to"
          :to="target(i)"
          class="sb-link"
          :class="{ active: isActive(i) }"
          active-class=""
          exact-active-class=""
          :aria-current="isActive(i) ? 'page' : undefined"
          :title="ui.collapsed ? i.label : undefined"
        >
          <Icon :name="i.icon" />
          <span class="sb-text">{{ i.label }}</span>
          <span v-if="i.badge === 'approvals' && live.pendingApprovals > 0" class="sb-badge" :aria-label="`${live.pendingApprovals} pending`">
            {{ live.pendingApprovals > 99 ? '99+' : live.pendingApprovals }}
          </span>
        </RouterLink>
      </template>
    </nav>
    <div class="sb-foot">
      <RouterLink v-if="live.killSwitch" to="/settings?tab=kill" class="sb-kill" title="Kill switch engaged">
        <Icon name="power" /><span class="sb-text">Kill switch engaged</span>
      </RouterLink>
      <div class="sb-status" :title="`Live updates: ${streamLabel}`">
        <span class="live-dot" :class="live.status" aria-hidden="true" />
        <span class="sb-text">{{ streamLabel }}</span>
        <span v-if="version" class="sb-text mono" style="margin-left: auto; font-size: 11px; opacity: 0.8">{{ /^\d/.test(version) ? `v${version}` : version }}</span>
      </div>
      <button type="button" class="sb-collapse" :aria-label="ui.collapsed ? 'Expand sidebar' : 'Collapse sidebar'" :aria-pressed="ui.collapsed" @click="ui.toggleCollapsed()">
        <Icon :name="ui.collapsed ? 'chevronsRight' : 'chevronsLeft'" /><span class="sb-text">Collapse</span>
      </button>
    </div>
  </aside>
</template>
