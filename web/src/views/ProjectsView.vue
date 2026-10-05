<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, type Project, type Task } from '../api'
import { useAuth } from '../stores/auth'
import { useCatalog } from '../stores/catalog'
import { useCoder } from '../stores/coder'
import { useLive } from '../stores/live'
import { safeUrl } from '../lib/format'
import { fetchAll } from '../lib/paged'
import { forgeIcon } from '../lib/forge'
import PageHeader from '../components/PageHeader.vue'
import EmptyState from '../components/EmptyState.vue'
import NewProjectDialog from '../components/NewProjectDialog.vue'
import Icon from '../components/Icon'

const auth = useAuth()
const catalog = useCatalog()
const coder = useCoder()
const live = useLive()
const route = useRoute()
const router = useRouter()

const loading = ref(true)
const filter = ref('')
const openTasks = ref<Task[]>([])
const showNew = ref(false)

async function load() {
  await Promise.all([coder.loadProjects(true), coder.loadIntegrations(), catalog.loadAgents(), loadOpen()])
  loading.value = false
}

// Open coding work per project: the task list has no project filter, so filter active tasks here.
async function loadOpen() {
  try {
    const open = await fetchAll((page) => api.pageTasks({ status: 'queued,assigned,running', page, size: 200 }, { quiet: true }))
    openTasks.value = open.filter((t) => t.project_id)
  } catch {
    /* counts are optional */
  }
}
const openCount = computed(() => {
  const m = new Map<string, number>()
  for (const t of openTasks.value) if (t.project_id) m.set(t.project_id, (m.get(t.project_id) ?? 0) + 1)
  return m
})

const projects = computed(() => {
  const q = filter.value.trim().toLowerCase()
  const list = [...coder.projects].sort((a, b) => a.name.localeCompare(b.name))
  if (!q) return list
  return list.filter((p) => [p.name, p.owner, p.repo, p.description, p.trigger_label, p.sandbox_image].some((s) => s?.toLowerCase().includes(q)))
})

function target(p: Project): string {
  if (p.agent_id) return catalog.agentName(p.agent_id)
  if (p.selector?.length) return p.selector.join(', ')
  return 'any agent'
}

const noIntegrations = computed(() => auth.isAdmin && !loading.value && !coder.forges.length)

function openNew() {
  showNew.value = true
}
function closeNew() {
  showNew.value = false
  if (route.query.new) router.replace({ query: {} })
}

let off: (() => void) | null = null
let t: ReturnType<typeof setTimeout> | null = null
onMounted(() => {
  load()
  if (route.query.new && auth.isAdmin) openNew()
  off = live.on((ev) => {
    if (ev.type !== 'task.updated') return
    if (t) clearTimeout(t)
    t = setTimeout(loadOpen, 800)
  })
})
onUnmounted(() => {
  off?.()
  if (t) clearTimeout(t)
})
</script>

<template>
  <div>
    <PageHeader title="Projects" subtitle="Repositories agents work on: each task gets its own branch, commits and a pull request for you to review.">
      <template v-if="auth.isAdmin">
        <RouterLink v-if="noIntegrations" to="/integrations" class="btn"><Icon name="plug" />Add an integration</RouterLink>
        <button type="button" class="btn btn-primary" @click="openNew"><Icon name="plus" />New project</button>
      </template>
    </PageHeader>

    <div v-if="coder.projects.length > 6" class="toolbar">
      <div class="search-input" style="width: min(320px, 100%)">
        <Icon name="search" />
        <input v-model="filter" class="input" type="search" placeholder="Search projects and repositories" aria-label="Search projects" />
      </div>
    </div>

    <div v-if="loading && !coder.projects.length" class="proj-grid" aria-busy="true">
      <div v-for="i in 3" :key="i" class="skel skel-card" />
    </div>

    <div v-else-if="!coder.projects.length" class="card">
      <EmptyState title="No projects yet" icon="repo">
        Connect a repository and agents can change it: they work on an <code>akili/…</code> branch, run the tests in a sandbox and open a pull request. The default branch is never pushed to.
        <template v-if="auth.isAdmin" #actions>
          <template v-if="noIntegrations">
            <RouterLink to="/integrations" class="btn btn-primary"><Icon name="plug" />Add an integration first</RouterLink>
          </template>
          <button v-else type="button" class="btn btn-primary" @click="openNew"><Icon name="link" />Connect a repository</button>
        </template>
        <template v-else #actions><span class="small muted">Ask an admin to connect a repository.</span></template>
      </EmptyState>
    </div>

    <div v-else-if="!projects.length" class="card"><EmptyState title="No matching projects" icon="search" compact>Try a different search.</EmptyState></div>

    <div v-else class="proj-grid">
      <article v-for="p in projects" :key="p.id" class="card proj-card stretch-card">
        <div class="pc-top">
          <span class="pc-forge" aria-hidden="true"><Icon :name="forgeIcon(coder.forgeOf(p))" /></span>
          <div class="grow" style="min-width: 0">
            <RouterLink :to="`/projects/${p.id}`" class="pc-name stretch-link truncate">{{ p.name }}</RouterLink>
            <a v-if="safeUrl(p.web_url)" :href="safeUrl(p.web_url)" target="_blank" rel="noopener noreferrer" class="pc-repo mono" :title="`Open ${p.owner}/${p.repo} on the forge`">
              {{ p.owner }}/{{ p.repo }}<Icon name="external" :size="12" />
            </a>
            <span v-else class="pc-repo mono">{{ p.owner }}/{{ p.repo }}</span>
          </div>
          <span v-if="openCount.get(p.id)" class="badge accent" :title="`${openCount.get(p.id)} open coding tasks`"><Icon name="loader" />{{ openCount.get(p.id) }} open</span>
        </div>
        <p v-if="p.description" class="pc-desc">{{ p.description }}</p>
        <div class="pc-meta">
          <span class="badge outline square" title="Default branch"><Icon name="gitBranch" />{{ p.default_branch }}</span>
          <span class="badge outline square" :title="p.agent_id ? 'Preferred agent' : p.selector?.length ? 'Label selector' : 'Runs on any agent'">
            <Icon :name="p.agent_id ? 'agents' : p.selector?.length ? 'layers' : 'globe'" />{{ target(p) }}
          </span>
          <span v-if="p.sandbox_image" class="badge info square" title="Sandbox image: tests run in this container"><Icon name="box" />{{ p.sandbox_image }}</span>
          <span v-else class="badge square" title="No sandbox image: sandbox_exec is disabled"><Icon name="box" />no sandbox</span>
          <span v-if="p.trigger_label" class="badge violet square" title="Issues with this label become tasks"><Icon name="tag" />{{ p.trigger_label }}</span>
        </div>
      </article>
    </div>

    <NewProjectDialog v-if="auth.isAdmin" :open="showNew" @close="closeNew" />
  </div>
</template>

<style scoped>
.proj-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(330px, 1fr));
  gap: 16px;
}
.proj-card {
  padding: 16px 18px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  transition:
    border-color var(--t) var(--ease),
    box-shadow var(--t) var(--ease);
}
.pc-top {
  display: flex;
  align-items: flex-start;
  gap: 12px;
}
.pc-forge {
  width: 38px;
  height: 38px;
  flex: none;
  border-radius: var(--radius);
  display: grid;
  place-items: center;
  background: var(--primary-50);
  color: var(--primary-text);
}
.pc-forge .icon {
  width: 20px;
  height: 20px;
}
.pc-name {
  display: block;
  font-weight: 650;
  font-size: 15px;
}
.pc-repo {
  position: relative;
  z-index: 1;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 12.5px;
  color: var(--text-tertiary);
  max-width: 100%;
  overflow-wrap: anywhere;
}
a.pc-repo:hover {
  color: var(--primary-text);
}
.pc-desc {
  margin: 0;
  font-size: 13px;
  color: var(--text-secondary);
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.pc-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: auto;
}
.pc-meta .badge {
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
}
@media (max-width: 480px) {
  .proj-grid {
    grid-template-columns: 1fr;
  }
}
</style>
