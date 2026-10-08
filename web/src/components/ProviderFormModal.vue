<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { ref, watch } from 'vue'
import { api, type Effort, type ModelProvider, type ProviderInput } from '../api'
import { useToast } from '../stores/toast'
import Modal from './Modal.vue'

/** Adds a provider, or edits `provider` when set. */
const props = defineProps<{ open: boolean; provider?: ModelProvider | null; makeDefault?: boolean }>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'saved', p: ModelProvider): void }>()
const toast = useToast()
const saving = ref(false)

const EFFORTS: Effort[] = ['', 'low', 'medium', 'high', 'xhigh', 'max']
function formFrom(p?: ModelProvider | null): ProviderInput {
  return {
    name: p?.name ?? '',
    kind: p?.kind ?? 'anthropic',
    base_url: p?.base_url ?? '',
    model: p?.model ?? '',
    effort: p?.effort ?? '',
    max_tokens: p?.max_tokens ?? 32000,
    context_tokens: p?.context_tokens ?? 0,
    api_key: '',
    is_default: p ? p.is_default : !!props.makeDefault,
    input_price_mtok: p?.input_price_mtok ?? 0,
    output_price_mtok: p?.output_price_mtok ?? 0,
  }
}
const form = ref<ProviderInput>(formFrom(props.provider))
watch(
  () => props.open,
  (o) => {
    if (o) form.value = formFrom(props.provider)
  },
)

async function save() {
  saving.value = true
  const f = form.value
  const b: ProviderInput = {
    ...f,
    name: f.name.trim(),
    model: f.model.trim(),
    base_url: f.base_url.trim(),
    max_tokens: Number(f.max_tokens) || 0,
    context_tokens: Math.max(0, Math.round(Number(f.context_tokens) || 0)),
    input_price_mtok: Number(f.input_price_mtok) || 0,
    output_price_mtok: Number(f.output_price_mtok) || 0,
  }
  try {
    const p = props.provider ? await api.updateProvider(props.provider.id, b) : await api.createProvider(b)
    toast.success(props.provider ? `Provider ${b.name} updated` : `Provider ${b.name} added`)
    emit('saved', p)
    emit('close')
  } catch {
    /* toasted */
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <Modal :open="open" :title="provider ? `Edit ${provider.name}` : 'Add provider'" wide :dismissable="!saving" @close="emit('close')">
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
        <input id="pv-key" v-model="form.api_key" class="input mono" type="password" autocomplete="new-password" :placeholder="provider?.has_key ? 'Leave empty to keep the current key' : ''" />
        <span class="hint">Write-only. {{ provider?.has_key ? 'A key is stored; leave empty to keep it.' : 'Not required for local endpoints.' }}</span>
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
        <label for="pv-ctx">Max context (tokens)</label>
        <input id="pv-ctx" v-model.number="form.context_tokens" class="input" type="number" min="0" step="1000" placeholder="200000" aria-describedby="pv-ctx-hint" />
        <span id="pv-ctx-hint" class="hint">The model's context window. Older tool output is dropped from long runs to fit it. 0 = 200,000.</span>
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
      <button type="button" class="btn" @click="emit('close')">Cancel</button>
      <button type="submit" form="provider-form" class="btn btn-primary" :disabled="saving || !form.name.trim() || !form.model.trim()">
        <span v-if="saving" class="spinner" />{{ provider ? 'Save' : 'Add provider' }}
      </button>
    </template>
  </Modal>
</template>
