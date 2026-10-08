<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, type ModelProvider, type TestResult } from '../../api'
import { useConfirm } from '../../stores/confirm'
import { useToast } from '../../stores/toast'
import Icon from '../../components/Icon'
import EmptyState from '../../components/EmptyState.vue'
import SkeletonRows from '../../components/SkeletonRows.vue'
import ProviderFormModal from '../../components/ProviderFormModal.vue'

const router = useRouter()
const confirm = useConfirm()
const toast = useToast()
const providers = ref<ModelProvider[]>([])
const loading = ref(true)
const show = ref(false)
const editing = ref<ModelProvider | null>(null)
const tests = ref<Record<string, TestResult | 'running'>>({})

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
  show.value = true
}

function openEdit(p: ModelProvider) {
  editing.value = p
  show.value = true
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

const open = (p: ModelProvider) => router.push(`/providers/${p.id}`)
const contextK = (p: ModelProvider) => `${((p.context_tokens || 200000) / 1000).toLocaleString()}k`

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
            <tr><th>Name</th><th>Model</th><th class="right">Context</th><th>Key</th><th><span class="sr-only">Actions</span></th></tr>
          </thead>
          <tbody>
            <SkeletonRows v-if="loading" :cols="5" :rows="2" />
            <tr v-else-if="!providers.length">
              <td colspan="5">
                <EmptyState title="No model providers yet" icon="cpu">
                  Agents cannot think until you add one. Anthropic, any OpenAI-compatible endpoint (OpenAI, Ollama, vLLM) or a scripted test provider.
                  <template #actions><button type="button" class="btn btn-primary" @click="openNew"><Icon name="plus" />Add provider</button></template>
                </EmptyState>
              </td>
            </tr>
            <tr v-for="p in providers" :key="p.id" class="clickable" tabindex="0" @click="open(p)" @keydown.enter.self="open(p)">
              <td>
                <span class="cell-title">{{ p.name }}</span>
                <span v-if="p.is_default" class="badge accent" style="margin-left: 6px"><Icon name="zap" />default</span>
                <div class="cell-sub">
                  {{ p.kind }}<template v-if="p.base_url"> · <span class="mono">{{ p.base_url }}</span></template>
                </div>
                <div v-if="tests[p.id] && tests[p.id] !== 'running'" class="cell-sub">
                  <span class="badge" :class="(tests[p.id] as TestResult).ok ? 'ok' : 'danger'" :title="(tests[p.id] as TestResult).error">
                    <Icon :name="(tests[p.id] as TestResult).ok ? 'checkCircle' : 'xCircle'" />{{ (tests[p.id] as TestResult).ok ? `OK · ${(tests[p.id] as TestResult).latency_ms} ms` : 'test failed' }}
                  </span>
                </div>
              </td>
              <td class="mono small" style="word-break: break-all">{{ p.model }}<div v-if="p.effort" class="cell-sub">effort {{ p.effort }}</div></td>
              <td class="right num nowrap">{{ contextK(p) }}</td>
              <td><span class="badge" :class="p.has_key ? 'ok' : 'warn'"><Icon :name="p.has_key ? 'lock' : 'alert'" />{{ p.has_key ? 'stored' : 'none' }}</span></td>
              <td class="right nowrap" @click.stop>
                <div class="row end" style="gap: 4px">
                  <button type="button" class="btn btn-sm btn-ghost btn-icon" :disabled="tests[p.id] === 'running'" :aria-label="`Test ${p.name}`" title="Send a test request" @click="test(p)">
                    <span v-if="tests[p.id] === 'running'" class="spinner" /><Icon v-else name="zap" />
                  </button>
                  <button type="button" class="btn btn-sm btn-ghost btn-icon" :aria-label="`Edit ${p.name}`" title="Edit" @click="openEdit(p)"><Icon name="edit" /></button>
                  <button type="button" class="btn btn-sm btn-ghost btn-icon btn-danger-ghost" :aria-label="`Delete ${p.name}`" title="Delete" @click="remove(p)"><Icon name="trash" /></button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <ProviderFormModal :open="show" :provider="editing" :make-default="providers.length === 0" @close="show = false" @saved="load" />
  </div>
</template>
