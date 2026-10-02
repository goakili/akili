<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed } from 'vue'
import { useLive } from '../stores/live'
import PageHeader from '../components/PageHeader.vue'
import Icon from '../components/Icon'

const live = useLive()
const version = computed(() => (/^\d/.test(live.version) ? `v${live.version}` : live.version || 'unknown'))
</script>

<template>
  <PageHeader title="About Akili" subtitle="A security-first control plane for autonomous AI operator agents." />

  <div class="grid-2">
    <section class="card span-all">
      <div class="card-body about-hero">
        <img src="/logo-icon.svg" alt="" width="56" height="56" />
        <div class="stack tight">
          <div class="about-name">akili <span class="mono small muted">{{ version }}</span></div>
          <div class="strong">Default Deny. Explicit Allow. Always Auditable.</div>
          <p class="muted" style="margin: 0">
            Akili manages a fleet of AI agents that write code, operate servers and drive Miabi. Every model and tool
            call is checked against a signed policy, risky actions wait for approval, and everything is recorded in a
            tamper-evident audit log.
          </p>
        </div>
      </div>
    </section>

    <section class="card">
      <div class="card-head"><h2><Icon name="layers" />Components</h2></div>
      <div class="card-body">
        <dl class="kv">
          <dt>Control plane</dt><dd>Web UI, REST API and LLM gateway; state in Postgres, events in Redis.</dd>
          <dt>Agent</dt><dd>Runs on your servers, dials out over an encrypted tunnel and acts only within its signed policy.</dd>
          <dt>Proto</dt><dd>The wire contract both sides share: envelopes, tool catalog, policy engine.</dd>
        </dl>
      </div>
    </section>

    <section class="card">
      <div class="card-head"><h2><Icon name="info" />Details</h2></div>
      <div class="card-body">
        <dl class="kv">
          <dt>Version</dt><dd class="mono">{{ version }}</dd>
          <dt>Source</dt>
          <dd><a href="https://github.com/goakili/akili" target="_blank" rel="noopener noreferrer">github.com/goakili/akili</a></dd>
          <dt>License</dt>
          <dd><a href="https://www.gnu.org/licenses/agpl-3.0.html" target="_blank" rel="noopener noreferrer">AGPL-3.0-or-later</a></dd>
          <dt>API docs</dt><dd><a href="/docs" target="_blank" rel="noopener">/docs</a></dd>
          <dt>Author</dt><dd><a href="https://jkaninda.dev/" target="_blank" rel="noopener noreferrer">Jonas Kaninda</a></dd>
          <dt>Copyright</dt><dd>© 2026 <a href="https://jkaninda.dev/" target="_blank" rel="noopener noreferrer">Jonas Kaninda</a></dd>
        </dl>
      </div>
    </section>
  </div>
</template>

<style scoped>
.about-hero {
  display: flex;
  gap: 18px;
  align-items: flex-start;
}
.about-name {
  font-size: 22px;
  font-weight: 700;
  letter-spacing: -0.01em;
}
</style>
