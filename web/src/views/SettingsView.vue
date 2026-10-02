<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, type Component } from 'vue'
import { useRoute } from 'vue-router'
import { useAuth } from '../stores/auth'
import PageHeader from '../components/PageHeader.vue'
import Icon, { type IconName } from '../components/Icon'
import ProvidersTab from './settings/ProvidersTab.vue'
import UsersTab from './settings/UsersTab.vue'
import ApiKeysTab from './settings/ApiKeysTab.vue'
import UsageTab from './settings/UsageTab.vue'
import KillSwitchTab from './settings/KillSwitchTab.vue'
import AccountTab from './settings/AccountTab.vue'
import SecurityTab from './settings/SecurityTab.vue'
import LicenseTab from './settings/LicenseTab.vue'

const auth = useAuth()
const route = useRoute()

interface Section {
  id: string
  label: string
  icon: IconName
  admin: boolean
  group: 'Personal' | 'Organization'
  title: string
  desc: string
  comp?: Component
  href?: string
}

const SECTIONS: Section[] = [
  { id: 'account', label: 'Account', icon: 'user', admin: false, group: 'Personal', title: 'Account', desc: 'Your profile, password and email notifications.', comp: AccountTab },
  { id: 'keys', label: 'API keys', icon: 'key', admin: false, group: 'Personal', title: 'API keys', desc: 'Personal keys for scripts and CI (Authorization: Bearer ak_…). They act as you, capped by their scopes.', comp: ApiKeysTab },
  { id: 'providers', label: 'Model providers', icon: 'cpu', admin: true, group: 'Organization', title: 'Model providers', desc: 'LLM endpoints behind the gateway. API keys are encrypted at rest and never leave the control plane.', comp: ProvidersTab },
  { id: 'users', label: 'Users', icon: 'users', admin: true, group: 'Organization', title: 'Users', desc: 'Roles: viewer (read) < operator (chat, tasks, approvals) < admin (agents, policies, providers) < owner.', comp: UsersTab },
  { id: 'usage', label: 'Usage & spend', icon: 'gauge', admin: false, group: 'Organization', title: 'Usage & spend', desc: 'Model calls, tokens and cost per agent and model.', comp: UsageTab },
  { id: 'kill', label: 'Kill switch', icon: 'power', admin: true, group: 'Organization', title: 'Kill switch', desc: 'An emergency stop for the whole fleet.', comp: KillSwitchTab },
  { id: 'security', label: 'Security', icon: 'lock', admin: true, group: 'Organization', title: 'Security', desc: 'Encryption at rest, audit export to your SIEM, and how people and agents authenticate. Read-only: these are set in the control-plane environment and CLI.', comp: SecurityTab },
  { id: 'license', label: 'License', icon: 'award', admin: true, group: 'Organization', title: 'License', desc: 'The edition and the Akili Enterprise license. Only the owner can install or remove it.', comp: LicenseTab },
  { id: 'audit', label: 'Audit log', icon: 'scroll', admin: true, group: 'Organization', title: 'Audit log', desc: '', href: '/audit' },
]

const visible = computed(() => SECTIONS.filter((t) => !t.admin || auth.isAdmin))
const groups = computed(() => (['Personal', 'Organization'] as const).map((g) => ({ label: g, items: visible.value.filter((s) => s.group === g) })))
const current = computed(() => visible.value.find((t) => t.id === route.query.tab && t.comp) ?? visible.value[0])
</script>

<template>
  <div>
    <PageHeader title="Settings" :subtitle="auth.me?.organization?.name ? `Organization: ${auth.me.organization.name}` : undefined" />
    <div class="settings-layout">
      <nav class="subnav" aria-label="Settings sections">
        <template v-for="g in groups" :key="g.label">
          <div class="sn-group">{{ g.label }}</div>
          <RouterLink
            v-for="s in g.items"
            :key="s.id"
            :to="s.href ?? { path: '/settings', query: { tab: s.id } }"
            :class="{ active: current?.id === s.id && !s.href }"
            :aria-current="current?.id === s.id && !s.href ? 'page' : undefined"
          >
            <Icon :name="s.icon" />{{ s.label }}<Icon v-if="s.href" name="arrowRight" style="margin-left: auto" />
          </RouterLink>
        </template>
      </nav>
      <section v-if="current" :aria-labelledby="`sec-${current.id}`" style="min-width: 0">
        <div class="settings-section-head">
          <h2 :id="`sec-${current.id}`">{{ current.title }}</h2>
          <p v-if="current.desc">{{ current.desc }}</p>
        </div>
        <component :is="current.comp" :key="current.id" />
      </section>
    </div>
  </div>
</template>
