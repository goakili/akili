// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

import { createHash, randomBytes } from 'node:crypto'

/** A PKCE pair (RFC 7636, S256) and the state that ties the browser's redirect to this sign-in. */
export interface SignInRequest {
  verifier: string
  challenge: string
  state: string
}

export function newSignInRequest(): SignInRequest {
  const verifier = randomBytes(32).toString('base64url')
  return { verifier, challenge: challengeOf(verifier), state: randomBytes(24).toString('base64url') }
}

export function challengeOf(verifier: string): string {
  return createHash('sha256').update(verifier).digest('base64url')
}
