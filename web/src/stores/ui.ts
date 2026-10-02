// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

import { defineStore } from 'pinia'
import { computed, ref, watch } from 'vue'

export type ThemeChoice = 'light' | 'dark' | 'system'

const THEME_KEY = 'akili.theme'
const SIDEBAR_KEY = 'akili.sidebar'

// Storage can be unavailable (private windows, blocked site data): every access is guarded.
function readStorage(key: string): string | null {
  try {
    return window.localStorage.getItem(key)
  } catch {
    return null
  }
}
function writeStorage(key: string, value: string) {
  try {
    window.localStorage.setItem(key, value)
  } catch {
    /* not persisted; the choice still applies to this tab */
  }
}

function systemDark(): boolean {
  try {
    return window.matchMedia('(prefers-color-scheme: dark)').matches
  } catch {
    return false
  }
}

/** Shell state: theme, sidebar collapse, mobile drawer and the current page's display name. */
export const useUi = defineStore('ui', () => {
  const stored = readStorage(THEME_KEY)
  const theme = ref<ThemeChoice>(stored === 'light' || stored === 'dark' ? stored : 'system')
  const prefersDark = ref(systemDark())
  const collapsed = ref(readStorage(SIDEBAR_KEY) === 'collapsed')
  const drawerOpen = ref(false)
  /** A detail page's display name (agent name, task title) for the breadcrumb and <title>. */
  const crumb = ref('')

  const resolved = computed<'light' | 'dark'>(() => (theme.value === 'system' ? (prefersDark.value ? 'dark' : 'light') : theme.value))

  try {
    window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', (e) => (prefersDark.value = e.matches))
  } catch {
    /* old browsers: the system choice is read once */
  }

  watch(
    resolved,
    (t) => {
      document.documentElement.setAttribute('data-theme', t)
      document.documentElement.style.colorScheme = t
    },
    { immediate: true },
  )

  function setTheme(t: ThemeChoice) {
    theme.value = t
    writeStorage(THEME_KEY, t)
  }

  function cycleTheme() {
    setTheme(theme.value === 'light' ? 'dark' : theme.value === 'dark' ? 'system' : 'light')
  }

  function toggleCollapsed() {
    collapsed.value = !collapsed.value
    writeStorage(SIDEBAR_KEY, collapsed.value ? 'collapsed' : 'expanded')
  }

  return { theme, resolved, collapsed, drawerOpen, crumb, setTheme, cycleTheme, toggleCollapsed }
})
