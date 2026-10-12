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
