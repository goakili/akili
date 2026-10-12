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

/** Task branches the extension will fetch and check out; anything else is refused. */
export function validTaskBranch(branch: string): boolean {
  return /^akili\/[A-Za-z0-9_-]{1,64}$/.test(branch)
}

/**
 * Reads the project slug from a repository's .akili.json. Only the slug is used: the file comes
 * from the repository, so it must never choose the server, a token or anything else.
 */
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
