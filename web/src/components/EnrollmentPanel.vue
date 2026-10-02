<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import type { Enrollment } from '../api'
import { fmtDate } from '../lib/format'
import CopyField from './CopyField.vue'
import Icon from './Icon'
defineProps<{ enrollment: Enrollment }>()
</script>

<template>
  <div class="stack">
    <div class="banner warn" role="alert">
      <Icon name="key" />
      <div class="banner-body">
        <strong>Shown once.</strong> Copy the join token now: it cannot be displayed again. It expires {{ fmtDate(enrollment.expires_at) }} and can be used a single time.
      </div>
    </div>
    <CopyField label="1. Install on a Linux host" :value="enrollment.install_command" />
    <CopyField label="Or run with Docker" :value="enrollment.docker_command" />
    <CopyField label="Join token" :value="enrollment.join_token" />
    <details class="small muted">
      <summary>Control plane with a self-signed or private-CA certificate?</summary>
      <p style="margin: 6px 0 0">Give the agent the CA to trust. On a host, fetch the script with <code>curl --cacert ca.pem</code> and add
        <code>AKILI_CA_CERT=$PWD/ca.pem</code> before <code>sh</code>. With Docker, add <code>-e AKILI_CA_CERT_PEM="$(cat ca.pem)"</code>.
        The agent keeps a copy of the CA in its state directory.</p>
    </details>
    <p class="small muted" style="margin: 0">The agent appears as <strong>online</strong> here as soon as it connects.</p>
  </div>
</template>
