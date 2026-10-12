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

import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { router } from './router'
import { configureApi } from './api'
import { useAuth } from './stores/auth'
import { useToast } from './stores/toast'
import { useLive } from './stores/live'
import { useUi } from './stores/ui'
import '@fontsource/ibm-plex-sans/400.css'
import '@fontsource/ibm-plex-sans/500.css'
import '@fontsource/ibm-plex-sans/600.css'
import '@fontsource/ibm-plex-sans/700.css'
import '@fontsource-variable/manrope'
import './styles.css'

const app = createApp(App)
const pinia = createPinia()
app.use(pinia)
app.use(router)

const toast = useToast(pinia)
const auth = useAuth(pinia)
const live = useLive(pinia)
useUi(pinia) // applies the theme

configureApi({
  onError: (err) => toast.error(err.message),
  onUnauthorized: () => {
    const wasSignedIn = !!auth.user
    auth.clear()
    live.stop()
    const cur = router.currentRoute.value
    if (cur.name !== 'login') {
      if (wasSignedIn) toast.info('Your session has ended. Please sign in again.')
      router.replace({ name: 'login', query: cur.fullPath && cur.fullPath !== '/' ? { next: cur.fullPath } : {} })
    }
  },
})

app.mount('#app')
