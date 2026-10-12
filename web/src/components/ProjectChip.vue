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
// A compact "owner/repo · branch" chip for coding tasks and sessions.
import { computed } from 'vue'
import type { Project } from '../api'
import { useCoder } from '../stores/coder'
import Icon from './Icon'
import { forgeIcon } from '../lib/forge'

const props = defineProps<{
  project?: Project | null
  branch?: string
  /** Link the repo name to the project page. */
  link?: boolean
  /** Narrow places (task cards): repository name only; the branch truncates first. */
  compact?: boolean
}>()
const coder = useCoder()
const full = computed(() => (props.project ? `${props.project.owner}/${props.project.repo}` : 'project'))
const name = computed(() => (props.compact && props.project ? props.project.repo : full.value))
</script>

<template>
  <span class="pchip" :title="branch ? `${full} · ${branch}` : full">
    <span class="pc-repo">
      <Icon :name="forgeIcon(coder.forgeOf(project))" />
      <RouterLink v-if="link && project" :to="`/projects/${project.id}`" class="truncate">{{ name }}</RouterLink>
      <span v-else class="truncate">{{ name }}</span>
    </span>
    <span v-if="branch" class="pc-branch"><Icon name="gitBranch" /><span class="truncate mono">{{ branch }}</span></span>
  </span>
</template>

<style scoped>
.pchip {
  display: inline-flex;
  align-items: stretch;
  max-width: 100%;
  min-width: 0;
  height: 22px;
  border: 1px solid var(--border-primary);
  border-radius: var(--radius-sm);
  font-size: 11.5px;
  line-height: 1;
  overflow: hidden;
  background: var(--bg-secondary);
  color: var(--text-secondary);
  vertical-align: middle;
}
.pchip .icon {
  width: 12px;
  height: 12px;
  flex: none;
}
.pc-repo,
.pc-branch {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 0 7px;
  min-width: 0;
}
/* The repository name is the useful part; the (long, generated) branch gives way first. */
.pc-repo {
  flex: 0 1 auto;
  max-width: 70%;
}
.pc-branch {
  flex: 1 1 0;
}
.pc-repo a {
  color: inherit;
}
.pc-repo a:hover {
  color: var(--primary-text);
}
.pc-branch {
  border-left: 1px solid var(--border-primary);
  background: var(--bg-primary);
  color: var(--text-tertiary);
}
</style>
