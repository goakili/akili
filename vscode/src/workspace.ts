// Copyright (c) 2026 Jonas Kaninda.
// SPDX-License-Identifier: Apache-2.0

/** Task branches the extension will fetch and check out; anything else is refused. */
export function validTaskBranch(branch: string): boolean {
  return /^akili\/[A-Za-z0-9_-]{1,64}$/.test(branch)
}

/** Only the slug is read: .akili.json comes from the repository, so it must never choose the server or a token. */
export function projectSlugFrom(text: string): string | undefined {
  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch {
    return undefined
  }
  const slug = (parsed as { project?: unknown } | null)?.project
  return typeof slug === 'string' && /^[a-z0-9][a-z0-9-]{0,119}$/.test(slug) ? slug : undefined
}

/** The control plane URL from settings: https, or http only for localhost. */
export function normalizeServerUrl(raw: string): string | undefined {
  let u: URL
  try {
    u = new URL(raw.trim())
  } catch {
    return undefined
  }
  const local = u.hostname === 'localhost' || u.hostname === '127.0.0.1' || u.hostname === '[::1]'
  if (u.protocol !== 'https:' && !(u.protocol === 'http:' && local)) return undefined
  if (u.username || u.password || u.search || u.hash) return undefined
  return u.origin + u.pathname.replace(/\/+$/, '')
}
