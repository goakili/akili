<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api, type AuditLog, type VerifyResult } from '../../api'
import { useCatalog } from '../../stores/catalog'
import { fmtDate } from '../../lib/format'
import JsonBlock from '../../components/JsonBlock'
import EmptyState from '../../components/EmptyState.vue'
import SkeletonRows from '../../components/SkeletonRows.vue'
import Icon from '../../components/Icon'
import { useRoute } from 'vue-router'

const items = ref<AuditLog[]>([])
const total = ref(0)
const page = ref(0)
const size = ref(50)
const action = ref('')
const actor = ref('')
const route = useRoute()
const target = ref(typeof route.query.target === 'string' ? route.query.target : '')
const loading = ref(false)
const open = ref<Set<number>>(new Set())
const verify = ref<VerifyResult | null>(null)
const verifying = ref(false)
const catalog = useCatalog()
const users = ref<Map<string, string>>(new Map())

// Show people and agents by name; ids stay available on hover.
function actorName(r: AuditLog): string {
  if (!r.actor_id) return '—'
  if (r.actor_type === 'agent') return catalog.agentName(r.actor_id)
  if (r.actor_type === 'user') return users.value.get(r.actor_id) ?? r.actor_id
  return r.actor_id
}
function targetName(r: AuditLog): string {
  if (r.target_type === 'agent') return catalog.agentName(r.target_id)
  if (r.target_type === 'user') return users.value.get(r.target_id) ?? r.target_id
  return r.target_id
}
async function loadNames() {
  catalog.loadAgents()
  try {
    users.value = new Map(((await api.listUsers()) ?? []).map((u) => [u.id, u.email]))
  } catch {
    /* ids are shown instead */
  }
}

const pages = computed(() => Math.max(1, Math.ceil(total.value / size.value)))

async function load() {
  loading.value = true
  try {
    const r = await api.audit({ page: page.value, size: size.value, action: action.value.trim() || undefined, actor_id: actor.value.trim() || undefined, target_id: target.value.trim() || undefined })
    items.value = r.items ?? []
    total.value = r.total
  } catch {
    /* toasted */
  } finally {
    loading.value = false
  }
}

function search() {
  page.value = 0
  load()
}

function go(p: number) {
  page.value = Math.min(Math.max(0, p), pages.value - 1)
  load()
}

function toggle(id: number) {
  const s = new Set(open.value)
  if (s.has(id)) s.delete(id)
  else s.add(id)
  open.value = s
}

async function runVerify() {
  verifying.value = true
  try {
    verify.value = await api.verifyAudit()
  } catch {
    /* toasted */
  } finally {
    verifying.value = false
  }
}

onMounted(() => {
  load()
  loadNames()
})
</script>

<template>
  <div class="stack">
    <div class="row wrap between">
      <form class="row wrap" @submit.prevent="search">
        <label for="au-action" class="sr-only">Action prefix</label>
        <input id="au-action" v-model="action" class="input mono" placeholder="action prefix, e.g. tool." style="width: 200px" />
        <label for="au-actor" class="sr-only">Actor ID</label>
        <input id="au-actor" v-model="actor" class="input mono" placeholder="actor id" style="width: 200px" />
        <label for="au-target" class="sr-only">Target ID</label>
        <input id="au-target" v-model="target" class="input mono" placeholder="target id" style="width: 200px" />
        <button type="submit" class="btn"><Icon name="search" />Filter</button>
      </form>
      <div class="row wrap">
        <span v-if="verify" class="badge" :class="verify.valid ? 'ok' : 'danger'" role="status">
          <Icon :name="verify.valid ? 'checkCircle' : 'xCircle'" />{{ verify.valid ? `Chain valid · ${verify.checked} rows checked` : `Broken at #${verify.broken_at} · ${verify.reason ?? ''}` }}
        </span>
        <button type="button" class="btn" :disabled="verifying" @click="runVerify"><span v-if="verifying" class="spinner" /><Icon v-else name="lock" />Verify chain</button>
      </div>
    </div>
    <div class="card">
      <div class="table-wrap">
        <table class="table">
          <thead><tr><th>#</th><th>Time</th><th>Actor</th><th>Action</th><th>Target</th><th>IP</th></tr></thead>
          <tbody>
            <SkeletonRows v-if="loading && !items.length" :cols="6" :rows="6" />
            <tr v-else-if="!items.length"><td colspan="6"><EmptyState title="No audit entries" icon="scroll" compact>Nothing matches these filters.</EmptyState></td></tr>
            <template v-for="r in items" :key="r.id">
              <tr class="clickable" tabindex="0" :aria-expanded="open.has(r.id)" @click="toggle(r.id)" @keydown.enter="toggle(r.id)">
                <td class="mono small muted">{{ r.id }}</td>
                <td class="nowrap">{{ fmtDate(r.created_at) }}</td>
                <td>
                  <span class="badge outline">{{ r.actor_type }}</span>
                  <span class="small" style="margin-left: 6px" :title="r.actor_id">{{ actorName(r) }}</span>
                </td>
                <td class="mono small" style="font-weight: 600">{{ r.action }}</td>
                <td class="small"><span class="muted">{{ r.target_type }}</span> <span :class="{ mono: targetName(r) === r.target_id }" :title="r.target_id">{{ targetName(r) }}</span></td>
                <td class="mono small">{{ r.ip || '—' }}</td>
              </tr>
              <tr v-if="open.has(r.id)" class="expanded">
                <td colspan="6">
                  <div class="stack tight">
                    <JsonBlock :value="r.metadata ?? {}" />
                    <div class="small muted mono truncate">hash {{ r.hash }} · prev {{ r.prev_hash || '—' }}</div>
                  </div>
                </td>
              </tr>
            </template>
          </tbody>
        </table>
      </div>
      <div class="pager">
        <span>{{ total.toLocaleString() }} entries · page {{ page + 1 }} of {{ pages }}</span>
        <div class="row">
          <label for="au-size" class="sr-only">Page size</label>
          <select id="au-size" v-model.number="size" class="select sm" style="width: 90px" @change="search">
            <option :value="25">25</option>
            <option :value="50">50</option>
            <option :value="100">100</option>
            <option :value="200">200</option>
          </select>
          <button type="button" class="btn btn-sm" :disabled="page === 0 || loading" @click="go(page - 1)">Previous</button>
          <button type="button" class="btn btn-sm" :disabled="page >= pages - 1 || loading" @click="go(page + 1)">Next</button>
        </div>
      </div>
    </div>
  </div>
</template>
