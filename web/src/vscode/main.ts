// Copyright (c) 2026 Jonas Kaninda.
//
// This file is part of Akili.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.
//
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
