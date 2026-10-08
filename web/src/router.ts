// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import type { Role } from './api'
import { useAuth } from './stores/auth'
import { useUi } from './stores/ui'

declare module 'vue-router' {
  interface RouteMeta {
    public?: boolean
    minRole?: Role
    title?: string
    /** Breadcrumb parent for detail pages. */
    parent?: { label: string; to: string }
    /** Pages that manage their own height (chat): no bottom padding, fills the viewport. */
    fill?: boolean
    /** Signed in, but shown without the app chrome (the editor sign-in page). */
    bare?: boolean
  }
}

const routes: RouteRecordRaw[] = [
  { path: '/login', name: 'login', component: () => import('./views/LoginView.vue'), meta: { public: true, title: 'Sign in' } },
  { path: '/', name: 'overview', component: () => import('./views/OverviewView.vue'), meta: { title: 'Dashboard' } },
  { path: '/agents', name: 'agents', component: () => import('./views/AgentsView.vue'), meta: { title: 'Agents' } },
  { path: '/agents/:id/terminal', name: 'terminal', component: () => import('./views/TerminalView.vue'), props: true, meta: { title: 'Terminal', minRole: 'admin', parent: { label: 'Agents', to: '/agents' }, fill: true } },
  { path: '/agents/:id', name: 'agent', component: () => import('./views/AgentDetailView.vue'), props: true, meta: { title: 'Agent', parent: { label: 'Agents', to: '/agents' } } },
  { path: '/projects', name: 'projects', component: () => import('./views/ProjectsView.vue'), meta: { title: 'Projects' } },
  { path: '/projects/:id', name: 'project', component: () => import('./views/ProjectDetailView.vue'), props: true, meta: { title: 'Project', parent: { label: 'Projects', to: '/projects' } } },
  { path: '/sessions', name: 'sessions', component: () => import('./views/SessionsView.vue'), meta: { title: 'Chat & sessions' } },
  { path: '/sessions/:id', name: 'session', component: () => import('./views/SessionView.vue'), props: true, meta: { title: 'Session', parent: { label: 'Chat & sessions', to: '/sessions' }, fill: true } },
  { path: '/tasks', name: 'tasks', component: () => import('./views/TasksView.vue'), meta: { title: 'Tasks' } },
  { path: '/tasks/:id', name: 'task', component: () => import('./views/TaskDetailView.vue'), props: true, meta: { title: 'Task', parent: { label: 'Tasks', to: '/tasks' } } },
  { path: '/schedules', name: 'schedules', component: () => import('./views/SchedulesView.vue'), meta: { title: 'Schedules' } },
  { path: '/changes', name: 'changes', component: () => import('./views/ChangesView.vue'), meta: { title: 'Changes' } },
  { path: '/changes/:id', name: 'change', component: () => import('./views/ChangeDetailView.vue'), props: true, meta: { title: 'Change', parent: { label: 'Changes', to: '/changes' } } },
  { path: '/terminals/:id', name: 'recording', component: () => import('./views/RecordingView.vue'), props: true, meta: { title: 'Terminal recording', minRole: 'admin', parent: { label: 'Agents', to: '/agents' } } },
  { path: '/alerts', name: 'alerts', component: () => import('./views/AlertRoutesView.vue'), meta: { title: 'Alerts', minRole: 'admin' } },
  { path: '/miabi', name: 'miabi', component: () => import('./views/MiabiView.vue'), meta: { title: 'Miabi' } },
  { path: '/chat', name: 'chat', component: () => import('./views/ChatView.vue'), meta: { title: 'Chat' } },
  { path: '/approvals', name: 'approvals', component: () => import('./views/ApprovalsView.vue'), meta: { title: 'Approvals' } },
  { path: '/lessons', name: 'lessons', component: () => import('./views/LessonsView.vue'), meta: { title: 'Lessons' } },
  { path: '/skills', name: 'skills', component: () => import('./views/SkillsView.vue'), meta: { title: 'Skills' } },
  { path: '/policies', name: 'policies', component: () => import('./views/PoliciesView.vue'), meta: { title: 'Policies' } },
  { path: '/audit', name: 'audit', component: () => import('./views/AuditView.vue'), meta: { title: 'Audit log', minRole: 'admin' } },
  { path: '/mcp', name: 'mcp', component: () => import('./views/MCPView.vue'), meta: { title: 'MCP servers', minRole: 'admin' } },
  { path: '/integrations', name: 'integrations', component: () => import('./views/IntegrationsView.vue'), meta: { title: 'Integrations', minRole: 'admin' } },
  { path: '/vscode/authorize', name: 'vscode-authorize', component: () => import('./views/VSCodeAuthorizeView.vue'), meta: { title: 'Sign in to your editor', bare: true } },
  { path: '/about', name: 'about', component: () => import('./views/AboutView.vue'), meta: { title: 'About' } },
  { path: '/providers/:id', name: 'provider', component: () => import('./views/ProviderDetailView.vue'), props: true, meta: { title: 'Model provider', minRole: 'admin', parent: { label: 'Model providers', to: '/settings?tab=providers' } } },
  { path: '/settings', name: 'settings', component: () => import('./views/SettingsView.vue'), meta: { title: 'Settings' } },
  { path: '/:pathMatch(.*)*', name: 'not-found', component: () => import('./views/NotFoundView.vue'), meta: { title: 'Not found' } },
]

export const router = createRouter({
  history: createWebHistory(),
  routes,
  scrollBehavior: (_to, _from, saved) => saved ?? { top: 0 },
})

router.beforeEach(async (to, from) => {
  const auth = useAuth()
  if (!auth.loaded) await auth.load()
  if (to.path !== from.path) useUi().crumb = ''
  if (to.meta.public) {
    if (to.name === 'login' && auth.user) return typeof to.query.next === 'string' ? to.query.next : '/'
    return true
  }
  if (!auth.user) return { name: 'login', query: to.fullPath !== '/' ? { next: to.fullPath } : {} }
  if (to.meta.minRole && !auth.can(to.meta.minRole)) return '/'
  return true
})
