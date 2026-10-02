<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api, type Effort, type ModelProvider, type ProviderInput, type TestResult } from '../../api'
import { useConfirm } from '../../stores/confirm'
import { useToast } from '../../stores/toast'
import Modal from '../../components/Modal.vue'
import Icon from '../../components/Icon'
import EmptyState from '../../components/EmptyState.vue'
import SkeletonRows from '../../components/SkeletonRows.vue'

const confirm = useConfirm()
const toast = useToast()
const providers = ref<ModelProvider[]>([])
const loading = ref(true)
const show = ref(false)
const editing = ref<ModelProvider | null>(null)
const saving = ref(false)
const tests = ref<Record<string, TestResult | 'running'>>({})

const EFFORTS: Effort[] = ['', 'low', 'medium', 'high', 'xhigh', 'max']
const blank = (): ProviderInput => ({
  name: '',
  kind: 'anthropic',
  base_url: '',
  model: '',
  effort: '',
  max_tokens: 32000,
  api_key: '',
  is_default: false,
  input_price_mtok: 0,
  output_price_mtok: 0,
})
const form = ref<ProviderInput>(blank())

async function load() {
  try {
    providers.value = (await api.listProviders()) ?? []
  } catch {
    /* toasted */
  } finally {
    loading.value = false
  }
}

function openNew() {
  editing.value = null
  form.value = { ...blank(), is_default: providers.value.length === 0 }
  show.value = true
}

function openEdit(p: ModelProvider) {
  editing.value = p
  form.value = {
    name: p.name,
    kind: p.kind,
    base_url: p.base_url,
    model: p.model,
    effort: p.effort,
    max_tokens: p.max_tokens,
    api_key: '',
    is_default: p.is_default,
    input_price_mtok: p.input_price_mtok,
    output_price_mtok: p.output_price_mtok,
  }
  show.value = true
}

async function save() {
  saving.value = true
  const b = { ...form.value, max_tokens: Number(form.value.max_tokens) || 0, input_price_mtok: Number(form.value.input_price_mtok) || 0, output_price_mtok: Number(form.value.output_price_mtok) || 0 }
  try {
    if (editing.value) await api.updateProvider(editing.value.id, b)
    else await api.createProvider(b)
    show.value = false
    toast.success(editing.value ? `Provider ${b.name} updated` : `Provider ${b.name} added`)
    load()
  } catch {
    /* toasted */
  } finally {
    saving.value = false
  }
}

async function remove(p: ModelProvider) {
  if (!(await confirm.ask({ title: `Delete provider ${p.name}?`, message: 'Providers bound to agents cannot be deleted.', confirmText: `Delete ${p.name}`, danger: true }))) return
  try {
    await api.deleteProvider(p.id)
    toast.success('Provider deleted')
    load()
  } catch {
    /* toasted */
  }
}

async function test(p: ModelProvider) {
  tests.value[p.id] = 'running'
  try {
    tests.value[p.id] = await api.testProvider(p.id)
  } catch {
    delete tests.value[p.id]
  }
}

onMounted(load)
</script>

<template>
  <div class="stack">
    <div class="row end">
      <button type="button" class="btn btn-primary" @click="openNew"><Icon name="plus" />Add provider</button>
    </div>
    <div class="card">
      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr><th>Name</th><th>Kind</th><th>Model</th><th>Effort</th><th>Key</th><th class="right">Price in/out (per MTok)</th><th>Test</th><th><span class="sr-only">Actions</span></th></tr>
          </thead>
          <tbody>
            <SkeletonRows v-if="loading" :cols="8" :rows="2" />
            <tr v-else-if="!providers.length">
              <td colspan="8">
                <EmptyState title="No model providers yet" icon="cpu">
                  Agents cannot think until you add one. Anthropic, any OpenAI-compatible endpoint (OpenAI, Ollama, vLLM) or a scripted test provider.
                  <template #actions><button type="button" class="btn btn-primary" @click="openNew"><Icon name="plus" />Add provider</button></template>
                </EmptyState>
              </td>
            </tr>
            <tr v-for="p in providers" :key="p.id">
              <td>
                <span class="cell-title">{{ p.name }}</span>
                <span v-if="p.is_default" class="badge accent" style="margin-left: 6px"><Icon name="zap" />default</span>
                <div v-if="p.base_url" class="cell-sub mono">{{ p.base_url }}</div>
              </td>
              <td>{{ p.kind }}</td>
              <td class="mono small">{{ p.model }}</td>
              <td>{{ p.effort || '—' }}</td>
              <td><span class="badge" :class="p.has_key ? 'ok' : 'warn'"><Icon :name="p.has_key ? 'lock' : 'alert'" />{{ p.has_key ? 'stored' : 'none' }}</span></td>
              <td class="right nowrap">${{ p.input_price_mtok }} / ${{ p.output_price_mtok }}</td>
              <td style="max-width: 260px">
                <span v-if="tests[p.id] === 'running'" class="row small muted"><span class="spinner" />testing…</span>
                <span v-else-if="tests[p.id]" class="badge" :class="(tests[p.id] as TestResult).ok ? 'ok' : 'danger'" :title="(tests[p.id] as TestResult).error">
                  <Icon :name="(tests[p.id] as TestResult).ok ? 'checkCircle' : 'xCircle'" />{{ (tests[p.id] as TestResult).ok ? `OK · ${(tests[p.id] as TestResult).latency_ms} ms` : 'failed' }}
                </span>
              </td>
              <td class="right nowrap">
                <div class="row end" style="gap: 4px">
                  <button type="button" class="btn btn-sm" :disabled="tests[p.id] === 'running'" @click="test(p)"><Icon name="zap" />Test</button>
                  <button type="button" class="btn btn-sm btn-ghost btn-icon" :aria-label="`Edit ${p.name}`" title="Edit" @click="openEdit(p)"><Icon name="edit" /></button>
                  <button type="button" class="btn btn-sm btn-ghost btn-icon btn-danger-ghost" :aria-label="`Delete ${p.name}`" title="Delete" @click="remove(p)"><Icon name="trash" /></button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <Modal :open="show" :title="editing ? `Edit ${editing.name}` : 'Add provider'" wide :dismissable="!saving" @close="show = false">
      <form id="provider-form" class="grid-2" @submit.prevent="save">
        <div class="field">
          <label for="pv-name">Name <span class="req">*</span></label>
          <input id="pv-name" v-model="form.name" class="input" required />
        </div>
        <div class="field">
          <label for="pv-kind">Kind <span class="req">*</span></label>
          <select id="pv-kind" v-model="form.kind" class="select">
            <option value="anthropic">Anthropic</option>
            <option value="openai">OpenAI-compatible (OpenAI, Ollama, vLLM…)</option>
            <option value="fake">Fake (scripted, for testing)</option>
          </select>
        </div>
        <div class="field">
          <label for="pv-model">Model <span class="req">*</span></label>
          <input id="pv-model" v-model="form.model" class="input mono" required />
        </div>
        <div class="field">
          <label for="pv-url">Base URL</label>
          <input id="pv-url" v-model="form.base_url" class="input mono" :placeholder="form.kind === 'openai' ? 'e.g. http://localhost:11434/v1 (Ollama)' : 'Provider default'" />
          <span v-if="form.kind === 'openai'" class="hint">A bare host gets /v1 added.</span>
        </div>
        <div class="field span-all">
          <label for="pv-key">API key</label>
          <input id="pv-key" v-model="form.api_key" class="input mono" type="password" autocomplete="new-password" :placeholder="editing?.has_key ? 'Leave empty to keep the current key' : ''" />
          <span class="hint">Write-only. {{ editing?.has_key ? 'A key is stored; leave empty to keep it.' : 'Not required for local endpoints.' }}</span>
        </div>
        <div class="field">
          <label for="pv-effort">Effort</label>
          <select id="pv-effort" v-model="form.effort" class="select">
            <option v-for="e in EFFORTS" :key="e" :value="e">{{ e || 'default' }}</option>
          </select>
        </div>
        <div class="field">
          <label for="pv-max">Max output tokens</label>
          <input id="pv-max" v-model.number="form.max_tokens" class="input" type="number" min="0" />
        </div>
        <div class="field">
          <label for="pv-in">Input price (USD / MTok)</label>
          <input id="pv-in" v-model.number="form.input_price_mtok" class="input" type="number" min="0" step="0.01" />
        </div>
        <div class="field">
          <label for="pv-out">Output price (USD / MTok)</label>
          <input id="pv-out" v-model.number="form.output_price_mtok" class="input" type="number" min="0" step="0.01" />
        </div>
        <label class="check span-all"><input v-model="form.is_default" type="checkbox" />Organization default (used by agents without a provider)</label>
      </form>
      <template #footer>
        <button type="button" class="btn" @click="show = false">Cancel</button>
        <button type="submit" form="provider-form" class="btn btn-primary" :disabled="saving || !form.name.trim() || !form.model.trim()">
          <span v-if="saving" class="spinner" />{{ editing ? 'Save' : 'Add provider' }}
        </button>
      </template>
    </Modal>
  </div>
</template>
