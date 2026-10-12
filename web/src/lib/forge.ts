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

import type { ForgeKind } from '../api'
import type { IconName } from '../components/Icon'

export const FORGES: { kind: ForgeKind; label: string; icon: IconName }[] = [
  { kind: 'gitea', label: 'Gitea', icon: 'gitea' },
  { kind: 'github', label: 'GitHub', icon: 'github' },
  { kind: 'gitlab', label: 'GitLab', icon: 'gitlab' },
]

export const isForge = (k: string | null | undefined): k is ForgeKind => FORGES.some((f) => f.kind === k)

export const forgeLabel = (k: string | null | undefined) => FORGES.find((f) => f.kind === k)?.label ?? 'Gitea'

export const forgeIcon = (k: string | null | undefined): IconName => FORGES.find((f) => f.kind === k)?.icon ?? 'gitea'

/** GitLab calls pull requests merge requests and numbers them "!12". */
export const isMergeRequest = (kindOrUrl: string | null | undefined) => kindOrUrl === 'gitlab' || !!kindOrUrl?.includes('/-/merge_requests/')

/** "!12" or "#12". */
export const prNumber = (kindOrUrl: string | null | undefined, n: number) => (isMergeRequest(kindOrUrl) ? `!${n}` : `#${n}`)

/** "merge request" or "pull request". */
export const prNoun = (kindOrUrl: string | null | undefined) => (isMergeRequest(kindOrUrl) ? 'merge request' : 'pull request')

/** Guesses the forge from a repository URL, for viewers who can't read integrations. */
export function forgeFromUrl(url: string): ForgeKind {
  try {
    const host = new URL(url).hostname
    if (host.endsWith('github.com')) return 'github'
    if (host.includes('gitlab')) return 'gitlab'
  } catch {
    /* fall through */
  }
  return 'gitea'
}
