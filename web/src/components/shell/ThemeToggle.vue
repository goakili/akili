<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, ref } from 'vue'
import { useUi, type ThemeChoice } from '../../stores/ui'
import { menuKeys, usePopover } from '../../lib/popover'
import Icon, { type IconName } from '../Icon'

const ui = useUi()
const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLElement | null>(null)
const { open, toggle, close } = usePopover(root, trigger)

const OPTIONS: { value: ThemeChoice; label: string; icon: IconName }[] = [
  { value: 'light', label: 'Light', icon: 'sun' },
  { value: 'dark', label: 'Dark', icon: 'moon' },
  { value: 'system', label: 'System', icon: 'monitor' },
]
const icon = computed<IconName>(() => (ui.theme === 'system' ? 'monitor' : ui.resolved === 'dark' ? 'moon' : 'sun'))

function pick(t: ThemeChoice) {
  ui.setTheme(t)
  close()
  trigger.value?.focus()
}
</script>

<template>
  <div ref="root" class="popover-anchor">
    <button ref="trigger" type="button" class="icon-btn" :aria-label="`Theme: ${ui.theme}`" aria-haspopup="menu" :aria-expanded="open" @click="toggle">
      <Icon :name="icon" />
    </button>
    <div v-if="open" class="menu" role="menu" aria-label="Theme" style="min-width: 160px" @keydown="menuKeys">
      <button
        v-for="o in OPTIONS"
        :key="o.value"
        type="button"
        role="menuitem"
        class="menu-item"
        :class="{ on: ui.theme === o.value }"
        :aria-current="ui.theme === o.value ? 'true' : undefined"
        @click="pick(o.value)"
      >
        <Icon :name="o.icon" />{{ o.label }}
        <Icon v-if="ui.theme === o.value" name="check" style="margin-left: auto" />
      </button>
    </div>
  </div>
</template>
