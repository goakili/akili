// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api, type ForgeKind, type Integration, type Project, type ProjectTemplates } from '../api'
import { useAuth } from './auth'

/** Caches for coding work: projects (every role), integrations (admins) and the built-in templates. */
export const useCoder = defineStore('coder', () => {
  const projects = ref<Project[]>([])
  const projectsLoaded = ref(false)
  const integrations = ref<Integration[]>([])
  /** Git forges only: what a project can live on. */
  const forges = computed(() => integrations.value.filter((i) => i.kind === 'gitea' || i.kind === 'github'))
  const templates = ref<ProjectTemplates | null>(null)
  let projectsAt = 0
  let inflight: Promise<void> | null = null

  async function loadProjects(force = false): Promise<void> {
    if (!force && projectsLoaded.value && Date.now() - projectsAt < 5000) return
    if (inflight) return inflight
    inflight = (async () => {
      try {
        projects.value = (await api.listProjects({ quiet: true })) ?? []
        projectsAt = Date.now()
      } catch {
        /* keep previous */
      } finally {
        projectsLoaded.value = true
        inflight = null
      }
    })()
    return inflight
  }

  /** Integrations are admin-only; other roles get an empty list without an error toast. */
  async function loadIntegrations(): Promise<Integration[]> {
    if (!useAuth().isAdmin) return []
    try {
      integrations.value = (await api.listIntegrations({ quiet: true })) ?? []
    } catch {
      /* keep previous */
    }
    return integrations.value
  }

  async function loadTemplates(): Promise<ProjectTemplates> {
    if (templates.value) return templates.value
    try {
      templates.value = await api.projectTemplates({ quiet: true })
    } catch {
      return { templates: [], presets: [] }
    }
    return templates.value
  }

  function project(id: string | null | undefined): Project | undefined {
    return id ? projects.value.find((p) => p.id === id) : undefined
  }

  function upsertProject(p: Project) {
    const i = projects.value.findIndex((x) => x.id === p.id)
    if (i >= 0) projects.value[i] = p
    else projects.value.push(p)
  }

  function removeProject(id: string) {
    projects.value = projects.value.filter((p) => p.id !== id)
  }

  /**
   * The forge a project lives on. Admins know it from the integration; other roles cannot read
   * integrations, so it is inferred from the repository URL.
   */
  function forgeOf(p: Project | null | undefined): ForgeKind {
    if (!p) return 'gitea'
    if (p.forge) return p.forge
    const it = integrations.value.find((i) => i.id === p.integration_id)
    if (it && (it.kind === 'gitea' || it.kind === 'github')) return it.kind
    try {
      return new URL(p.web_url).hostname.endsWith('github.com') ? 'github' : 'gitea'
    } catch {
      return 'gitea'
    }
  }

  return { projects, projectsLoaded, integrations, forges, templates, loadProjects, loadIntegrations, loadTemplates, project, upsertProject, removeProject, forgeOf }
})
