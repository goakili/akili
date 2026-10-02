<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, ref } from 'vue'
import { useAuth } from '../../stores/auth'
import { useSound } from '../../stores/sound'
import { usePopover } from '../../lib/popover'
import type { SoundKind } from '../../lib/sounds'
import Icon from '../Icon'

const auth = useAuth()
const sound = useSound()
const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLElement | null>(null)
const { open, toggle } = usePopover(root, trigger)

const label = computed(() =>
  !sound.prefs.enabled ? 'Sounds: off' : sound.blocked ? 'Sounds: blocked until you click the page' : 'Sounds: on',
)
const volume = computed({
  get: () => Math.round(sound.prefs.volume * 100),
  set: (v: number) => (sound.prefs.volume = v / 100),
})

const TESTS: { kind: SoundKind; label: string }[] = [
  { kind: 'approval', label: 'Approval' },
  { kind: 'success', label: 'Succeeded' },
  { kind: 'failure', label: 'Failed' },
]
</script>

<template>
  <div ref="root" class="popover-anchor">
    <button ref="trigger" type="button" class="icon-btn" :aria-label="label" :title="label" aria-haspopup="dialog" :aria-expanded="open" @click="toggle">
      <Icon :name="sound.prefs.enabled ? 'volume' : 'volume-off'" />
      <span v-if="sound.blocked" class="sound-blocked" aria-hidden="true" />
    </button>
    <div v-if="open" class="menu" role="dialog" aria-label="Sounds" style="min-width: 260px">
      <div class="menu-head">
        <label class="switch"><input v-model="sound.prefs.enabled" type="checkbox" />Play sounds</label>
      </div>
      <div class="menu-sep" />
      <fieldset class="sound-options" :disabled="!sound.prefs.enabled">
        <label class="check" :title="auth.isOperator ? undefined : 'Only operators and admins can approve'">
          <input v-model="sound.prefs.approvals" type="checkbox" :disabled="!auth.isOperator" />Approval requests
        </label>
        <label class="check"><input v-model="sound.prefs.myTasks" type="checkbox" />My tasks finish</label>
        <label class="check"><input v-model="sound.prefs.allTasks" type="checkbox" />Any task finishes</label>
        <label class="sound-volume">
          <span>Volume</span>
          <input v-model.number="volume" type="range" min="0" max="100" step="5" :aria-valuetext="`${volume}%`" />
        </label>
        <div class="sound-tests">
          <button v-for="t in TESTS" :key="t.kind" type="button" class="btn btn-sm" @click="sound.test(t.kind)">
            <Icon name="play" :size="12" />{{ t.label }}
          </button>
        </div>
      </fieldset>
      <p class="sound-note">Only one open tab plays each sound. Settings are saved in this browser.</p>
    </div>
  </div>
</template>

<style scoped>
.sound-blocked {
  position: absolute;
  top: 6px;
  right: 6px;
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--warning-500);
  border: 2px solid var(--bg-primary);
}
.sound-options {
  display: grid;
  gap: 8px;
  padding: 6px 10px;
  margin: 0;
  border: 0;
  min-width: 0;
}
.sound-options:disabled {
  opacity: 0.55;
}
.check {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}
.sound-volume {
  display: grid;
  grid-template-columns: auto 1fr;
  align-items: center;
  gap: 10px;
  font-size: 13px;
}
.sound-volume input {
  accent-color: var(--primary-600);
}
.sound-tests {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.sound-note {
  margin: 4px 10px 6px;
  font-size: 12px;
  color: var(--text-tertiary);
}
</style>
