<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, ApiError, type ModelProvider, type ProviderDetail, type TestResult } from '../api'
import { useConfirm } from '../stores/confirm'
import { useToast } from '../stores/toast'
import { useUi } from '../stores/ui'
import { compact, fmtDate, num, usd } from '../lib/format'
import Badge from '../components/Badge.vue'
import PageHeader from '../components/PageHeader.vue'
import EmptyState from '../components/EmptyState.vue'
import ProviderFormModal from '../components/ProviderFormModal.vue'
import Icon from '../components/Icon'

const props = defineProps<{ id: string }>()
const router = useRouter()
const confirm = useConfirm()
const toast = useToast()
const ui = useUi()

const LIST = '/settings?tab=providers'
const KINDS: Record<string, string> = { anthropic: 'Anthropic', openai: 'OpenAI-compatible', fake: 'Fake (scripted)' }

const provider = ref<ProviderDetail | null>(null)
const notFound = ref(false)
const editing = ref(false)
const testResult = ref<TestResult | null>(null)
const testing = ref(false)

async function load() {
  try {
    provider.value = await api.getProvider(props.id, { quiet: true })
    ui.crumb = provider.value.name
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound.value = true
    else if (e instanceof ApiError) toast.error(e.message)
  }
}

async function test() {
  testing.value = true
  testResult.value = null
  try {
    testResult.value = await api.testProvider(props.id)
  } catch {
    /* toasted */
  } finally {
    testing.value = false
  }
}

async function makeDefault(p: ModelProvider) {
  try {
    await api.updateProvider(p.id, {
      name: p.name, kind: p.kind, base_url: p.base_url, model: p.model, effort: p.effort, max_tokens: p.max_tokens,
      context_tokens: p.context_tokens, api_key: '', is_default: true, input_price_mtok: p.input_price_mtok, output_price_mtok: p.output_price_mtok,
    })
    toast.success(`${p.name} is now the organization default`)
    load()
  } catch {
    /* toasted */
  }
}

async function remove(p: ModelProvider) {
  if (!(await confirm.ask({ title: `Delete provider ${p.name}?`, message: 'Providers bound to agents cannot be deleted.', confirmText: `Delete ${p.name}`, danger: true }))) return
  try {
    await api.deleteProvider(p.id)
    toast.success('Provider deleted')
    router.push(LIST)
  } catch {
    /* toasted */
  }
}

onMounted(load)
</script>

<template>
  <EmptyState v-if="notFound" title="Provider not found" icon="cpu">
    It may belong to another organization or have been deleted.
    <template #actions><RouterLink :to="LIST" class="btn">Back to model providers</RouterLink></template>
  </EmptyState>
  <div v-else-if="!provider" class="stack loose" aria-busy="true">
    <span class="skel lg" style="width: 320px" />
    <div class="skel skel-card" style="height: 200px" />
  </div>
  <div v-else class="stack loose">
    <PageHeader :title="provider.name" :back="{ to: LIST, label: 'Model providers' }" style="margin-bottom: 0">
      <template #badges>
        <span v-if="provider.is_default" class="badge accent"><Icon name="zap" />default</span>
        <span class="badge">{{ KINDS[provider.kind] ?? provider.kind }}</span>
      </template>
      <template #subtitle><span class="mono">{{ provider.model }}</span></template>
      <button type="button" class="btn" :disabled="testing" @click="test"><span v-if="testing" class="spinner" /><Icon v-else name="zap" />Test</button>
      <button v-if="!provider.is_default" type="button" class="btn" @click="makeDefault(provider)"><Icon name="checkCircle" />Make default</button>
      <button type="button" class="btn btn-primary" @click="editing = true"><Icon name="edit" />Edit</button>
      <button type="button" class="btn btn-ghost btn-icon btn-danger-ghost" :aria-label="`Delete ${provider.name}`" title="Delete" @click="remove(provider)"><Icon name="trash" /></button>
    </PageHeader>

    <div v-if="testResult" class="banner" :class="testResult.ok ? 'ok' : 'danger'" role="status">
      <Icon :name="testResult.ok ? 'checkCircle' : 'xCircle'" />
      <div class="banner-body">
        <template v-if="testResult.ok">The model answered in {{ num(testResult.latency_ms) }} ms<template v-if="testResult.reply">: “{{ testResult.reply }}”</template>.</template>
        <template v-else>Test failed: {{ testResult.error }}</template>
      </div>
    </div>

    <div class="detail-grid">
      <section class="card">
        <div class="card-head"><h2><Icon name="cpu" />Configuration</h2></div>
        <div class="card-body">
          <dl class="kv">
            <dt>Kind</dt><dd>{{ KINDS[provider.kind] ?? provider.kind }}</dd>
            <dt>Model</dt><dd class="mono">{{ provider.model }}</dd>
            <dt>Base URL</dt><dd class="mono small">{{ provider.base_url || 'Provider default' }}</dd>
            <dt>Effort</dt><dd>{{ provider.effort || 'default' }}</dd>
            <dt>Max output</dt><dd class="num">{{ num(provider.max_tokens) }} tokens</dd>
            <dt>Max context</dt>
            <dd class="num">
              {{ num(provider.context_tokens || 200000) }} tokens
              <span v-if="!provider.context_tokens" class="muted small">(default)</span>
            </dd>
            <dt>API key</dt>
            <dd><span class="badge" :class="provider.has_key ? 'ok' : 'warn'"><Icon :name="provider.has_key ? 'lock' : 'alert'" />{{ provider.has_key ? 'stored, encrypted' : 'none' }}</span></dd>
            <dt>Price in / out</dt>
            <dd class="num">
              <template v-if="provider.input_price_mtok || provider.output_price_mtok">{{ usd(provider.input_price_mtok) }} / {{ usd(provider.output_price_mtok) }} per MTok</template>
              <span v-else class="muted">list price</span>
            </dd>
            <dt>Created</dt><dd>{{ fmtDate(provider.created_at) }}</dd>
            <dt>Updated</dt><dd>{{ fmtDate(provider.updated_at) }}</dd>
            <dt>ID</dt><dd class="mono small">{{ provider.id }}</dd>
          </dl>
        </div>
      </section>
      <section class="card">
        <div class="card-head"><h2><Icon name="info" />Usage, last 30 days</h2></div>
        <div class="card-body">
          <dl class="kv">
            <dt>Model calls</dt><dd class="num">{{ num(provider.usage_30d.calls) }}</dd>
            <dt>Input tokens</dt><dd class="num">{{ compact(provider.usage_30d.input_tokens) }}</dd>
            <dt>Output tokens</dt><dd class="num">{{ compact(provider.usage_30d.output_tokens) }}</dd>
            <dt>Cost</dt><dd class="num">{{ usd(provider.usage_30d.cost_usd) }}</dd>
          </dl>
        </div>
      </section>
    </div>

    <section class="card">
      <div class="card-head"><h2><Icon name="agents" />Agents using this provider</h2></div>
      <div v-if="!provider.agents.length" class="card-body muted">
        No agent uses this provider.{{ provider.is_default ? '' : ' Pick it on an agent, or make it the organization default.' }}
      </div>
      <div v-else class="table-wrap">
        <table class="table">
          <thead><tr><th>Agent</th><th>Status</th><th>How</th></tr></thead>
          <tbody>
            <tr v-for="a in provider.agents" :key="a.id" class="clickable" tabindex="0" @click="router.push(`/agents/${a.id}`)" @keydown.enter="router.push(`/agents/${a.id}`)">
              <td><RouterLink :to="`/agents/${a.id}`" class="cell-title" @click.stop>{{ a.name }}</RouterLink></td>
              <td><Badge :value="a.status" /></td>
              <td class="muted small">{{ a.via_default ? 'organization default' : 'assigned' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <ProviderFormModal :open="editing" :provider="provider" @close="editing = false" @saved="load" />
  </div>
</template>
