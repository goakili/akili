<!--
  Copyright (c) 2026 Jonas Kaninda.

  This file is part of Akili.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published by
  the Free Software Foundation, either version 3 of the License, or
  (at your option) any later version.

  This program is distributed in the hope that it will be useful,
  but WITHOUT ANY WARRANTY; without even the implied warranty of
  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
  GNU Affero General Public License for more details.

  You should have received a copy of the GNU Affero General Public License
  along with this program.  If not, see <https://www.gnu.org/licenses/>.

  SPDX-License-Identifier: AGPL-3.0-or-later
-->
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
