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
// A pull request link: "#12" (GitLab: "!12") with the PR icon, opening the forge in a new tab.
import { computed } from 'vue'
import { safeUrl } from '../lib/format'
import { prNoun, prNumber } from '../lib/forge'
import Icon from './Icon'

const props = defineProps<{ url: string; number?: number; /** Button look instead of an inline chip. */ button?: boolean; label?: string }>()
const href = computed(() => safeUrl(props.url))
const ref = computed(() => (props.number ? prNumber(props.url, props.number) : ''))
const noun = computed(() => prNoun(props.url))
const text = computed(() => props.label ?? (ref.value || noun.value[0].toUpperCase() + noun.value.slice(1)))
</script>

<template>
  <a v-if="href" :href="href" target="_blank" rel="noopener noreferrer" :class="button ? 'btn btn-sm' : 'prlink'" :title="`Open ${noun} ${ref} on the forge`" @click.stop>
    <Icon name="gitPR" />{{ text }}<Icon name="external" class="ext" />
  </a>
</template>

<style scoped>
.prlink {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 22px;
  padding: 0 7px;
  border-radius: 999px;
  font-size: 11.5px;
  font-weight: 600;
  background: var(--violet-50);
  color: var(--violet-text);
  border: 1px solid color-mix(in srgb, var(--violet-500) 22%, transparent);
  white-space: nowrap;
  position: relative;
  z-index: 1;
}
.prlink:hover {
  text-decoration: none;
  border-color: var(--violet-500);
}
.prlink .icon {
  width: 12px;
  height: 12px;
}
.prlink .ext,
.btn .ext {
  opacity: 0.7;
}
</style>
