<!--
  Copyright (c) 2026 Jonas Kaninda.
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import PageHeader from '../components/PageHeader.vue'
import Icon, { type IconName } from '../components/Icon'
import GrantsPanel from './autonomy/GrantsPanel.vue'
import WindowsPanel from './autonomy/WindowsPanel.vue'
import RunbooksPanel from './autonomy/RunbooksPanel.vue'

type TabId = 'grants' | 'windows' | 'runbooks'
const TABS: { id: TabId; label: string; icon: IconName }[] = [
  { id: 'grants', label: 'Grants', icon: 'key' },
  { id: 'windows', label: 'Windows & freezes', icon: 'clock' },
  { id: 'runbooks', label: 'Runbooks', icon: 'skills' },
]
const route = useRoute()
const router = useRouter()
const tab = computed<TabId>(() => TABS.find((t) => t.id === route.query.tab)?.id ?? 'grants')

function setTab(id: TabId) {
  router.replace({ query: { ...route.query, tab: id === 'grants' ? undefined : id } })
}
</script>

<template>
  <div>
    <PageHeader title="Autonomy" subtitle="What agents may do without asking: time-boxed grants, change windows and freezes, and runbooks. Policy denials and critical calls are never covered." />
    <div class="tabs" role="tablist" aria-label="Autonomy sections">
      <button v-for="t in TABS" :id="`autab-${t.id}`" :key="t.id" type="button" role="tab" class="tab" :aria-selected="tab === t.id" :aria-controls="`aupanel-${t.id}`" @click="setTab(t.id)">
        <Icon :name="t.icon" />{{ t.label }}
      </button>
    </div>
    <div :id="`aupanel-${tab}`" role="tabpanel" :aria-labelledby="`autab-${tab}`">
      <GrantsPanel v-if="tab === 'grants'" />
      <WindowsPanel v-else-if="tab === 'windows'" />
      <RunbooksPanel v-else @granted="setTab('grants')" />
    </div>
  </div>
</template>
