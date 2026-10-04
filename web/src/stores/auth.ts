// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api, roleRank, ApiError, type MeResponse, type Role } from '../api'

export const useAuth = defineStore('auth', () => {
  const me = ref<MeResponse | null>(null)
  const loaded = ref(false)

  const user = computed(() => me.value?.user ?? null)
  const role = computed<Role | null>(() => me.value?.user.role ?? null)
  const isOperator = computed(() => roleRank(role.value) >= roleRank('operator'))
  const isAdmin = computed(() => roleRank(role.value) >= roleRank('admin'))

  function can(min: Role): boolean {
    return roleRank(role.value) >= roleRank(min)
  }

  /** Loads the current operator; returns false when not signed in. */
  async function load(): Promise<boolean> {
    try {
      me.value = await api.me({ noAuthRedirect: true, quiet: true })
      return true
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) me.value = null
      else if (!(e instanceof ApiError)) throw e
      return me.value !== null
    } finally {
      loaded.value = true
    }
  }

  /** Signs in with a password; returns a challenge token when a second factor is still needed. */
  async function login(email: string, password: string): Promise<string | null> {
    const res = await api.login(email, password)
    if (res.mfa_required && res.mfa_token) return res.mfa_token
    await load()
    return null
  }

  async function loginMFA(mfaToken: string, code: string) {
    await api.loginMFA(mfaToken, code)
    await load()
  }

  async function logout() {
    try {
      await api.logout()
    } finally {
      me.value = null
    }
  }

  function clear() {
    me.value = null
  }

  return { me, loaded, user, role, isOperator, isAdmin, can, load, login, loginMFA, logout, clear }
})
