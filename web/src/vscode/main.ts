// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// The Akili sidebar inside VS Code: the web UI's own components, with the API reached through the
// extension (bridge.ts). The bridge must load before anything calls fetch.
import { vscode } from './bridge'
import { createApp, h } from 'vue'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import App from './App.vue'
import { configureApi } from '../api'
import { useToast } from '../stores/toast'
import '../styles.css'
import './vscode.css'

// Follow the editor's theme: VS Code sets vscode-dark / vscode-light / vscode-high-contrast on <body>.
function applyTheme() {
  const dark = document.body.classList.contains('vscode-dark') || document.body.classList.contains('vscode-high-contrast')
  document.documentElement.setAttribute('data-theme', dark ? 'dark' : 'light')
  document.documentElement.style.colorScheme = dark ? 'dark' : 'light'
}
applyTheme()
new MutationObserver(applyTheme).observe(document.body, { attributes: true, attributeFilter: ['class'] })

// Links inside the transcript (tasks, agents, changes) point at web UI pages: open those in the browser.
const router = createRouter({
  history: createMemoryHistory(),
  routes: [
    { path: '/', component: App },
    { path: '/:rest(.*)*', component: App },
  ],
})
router.beforeEach((to, from) => {
  if (from.matched.length && to.path !== '/') {
    vscode.postMessage({ type: 'openWeb', path: to.fullPath })
    return false
  }
  return true
})

const app = createApp({ render: () => h(RouterView) })
const pinia = createPinia()
app.use(pinia)
app.use(router)
const toast = useToast(pinia)
configureApi({
  onError: (err) => toast.error(err.message),
  onUnauthorized: () => vscode.postMessage({ type: 'unauthorized' }),
})
app.mount('#app')
