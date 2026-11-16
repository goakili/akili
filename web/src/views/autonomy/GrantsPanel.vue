<!--
  Copyright (c) 2026 Jonas Kaninda.
  SPDX-License-Identifier: Apache-2.0
-->
<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { api, type ApprovalGrant, type Runbook } from '../../api'
import { useAuth } from '../../stores/auth'
import { useCatalog } from '../../stores/catalog'
import { useConfirm } from '../../stores/confirm'
import { useToast } from '../../stores/toast'
import { describeMatch } from '../../lib/grants'
import { fmtDate, relTime } from '../../lib/format'
import { useNow } from '../../lib/now'
import Badge from '../../components/Badge.vue'
import EmptyState from '../../components/EmptyState.vue'
import SkeletonRows from '../../components/SkeletonRows.vue'
import Icon from '../../components/Icon'

const props = withDefaults(defineProps<{ agentId?: string }>(), { agentId: '' })

const auth = useAuth()
const catalog = useCatalog()
const confirm = useConfirm()
const toast = useToast()
const now = useNow()

const grants = ref<ApprovalGrant[]>([])
const runbooks = ref<Runbook[]>([])
const activeOnly = ref(true)
const loading = ref(true)

async function load() {
  try {
    grants.value = (await api.listGrants({ agent_id: props.agentId || undefined, active: activeOnly.value || undefined })) ?? []
  } catch {
    /* toasted */
  } finally {
    loading.value = false
  }
}

function state(g: ApprovalGrant): string {
  if (g.revoked_at) return 'revoked'
  if (new Date(g.expires_at).getTime() <= now.value) return 'expired'
  if (g.max_uses > 0 && g.uses >= g.max_uses) return 'used up'
  return 'active'
}

const runbookName = computed(() => (id: string | null) => runbooks.value.find((r) => r.id === id)?.name ?? id ?? '')

function target(g: ApprovalGrant) {
  return g.agent_id ? catalog.agentName(g.agent_id) : `labels: ${(g.labels ?? []).join(', ')}`
}

async function revoke(g: ApprovalGrant) {
  if (!(await confirm.ask({ title: `Revoke this ${g.tool} grant?`, message: 'Matching calls will ask for approval again.', confirmText: 'Revoke', danger: true }))) return
  try {
    Object.assign(g, await api.revokeGrant(g.id))
    toast.success('Grant revoked')
    if (activeOnly.value) grants.value = grants.value.filter((x) => x.id !== g.id)
  } catch {
    /* toasted */
  }
}

watch(activeOnly, load)
onMounted(async () => {
  load()
  catalog.loadAgents()
  runbooks.value = (await api.listRunbooks({ quiet: true }).catch(() => null)) ?? []
})
</script>

<template>
  <div class="card">
    <div class="card-head">
      <h2><Icon name="key" />Grants</h2>
      <label class="switch"><input v-model="activeOnly" type="checkbox" />Active only</label>
    </div>
    <div class="table-wrap">
      <table class="table">
        <thead>
          <tr><th>Tool</th><th v-if="!agentId">Agents</th><th>Covers</th><th>Uses</th><th>Expires</th><th>Status</th><th class="hide-mobile">Reason</th><th><span class="sr-only">Actions</span></th></tr>
        </thead>
        <tbody>
          <SkeletonRows v-if="loading" :cols="8" :rows="2" />
          <tr v-else-if="!grants.length">
            <td colspan="8">
              <EmptyState :title="activeOnly ? 'No active grants' : 'No grants yet'" icon="key">
                Use “Approve and allow similar…” on an approval, accept a suggestion on an agent, or grant a runbook.
              </EmptyState>
            </td>
          </tr>
          <tr v-for="g in grants" :key="g.id">
            <td class="mono strong nowrap">{{ g.tool }}<div class="cell-sub">up to {{ g.max_risk }} risk{{ g.windowed ? ' · in windows only' : '' }}</div></td>
            <td v-if="!agentId" class="nowrap">{{ target(g) }}</td>
            <td class="small" style="max-width: 320px">{{ g.runbook_id ? `runbook ${runbookName(g.runbook_id)}${Object.keys(g.match ?? {}).length ? ' · ' + describeMatch(g.match) : ''}` : describeMatch(g.match) }}</td>
            <td class="nowrap">{{ g.uses }}{{ g.max_uses ? ` / ${g.max_uses}` : '' }}<div v-if="g.last_used_at" class="cell-sub">last {{ relTime(g.last_used_at, now) }}</div></td>
            <td class="nowrap" :title="fmtDate(g.expires_at)">{{ relTime(g.expires_at, now) }}</td>
            <td><Badge :value="state(g)" /></td>
            <td class="truncate hide-mobile" style="max-width: 240px" :title="g.reason">{{ g.reason }}</td>
            <td class="right">
              <button v-if="auth.isOperator && state(g) === 'active'" type="button" class="btn btn-sm btn-ghost btn-danger-ghost" @click="revoke(g)"><Icon name="ban" />Revoke</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
