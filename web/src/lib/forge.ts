// SPDX-FileCopyrightText: 2026 Jonas Kaninda
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
