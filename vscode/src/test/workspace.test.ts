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

import { test } from 'node:test'
import * as assert from 'node:assert/strict'
import { challengeOf, newSignInRequest } from '../pkce'
import { normalizeServerUrl, projectSlugFrom, validTaskBranch } from '../workspace'

test('task branches', () => {
  assert.ok(validTaskBranch('akili/tsk_abc123'))
  for (const b of ['main', 'akili/', 'akili/a/b', 'akili/x;rm -rf', '-akili/x', 'akili/..', 'akili/x y']) assert.ok(!validTaskBranch(b), b)
})

test('.akili.json gives only a slug', () => {
  assert.equal(projectSlugFrom('{"project":"simple-api"}'), 'simple-api')
  assert.equal(projectSlugFrom('{"project":"simple-api","url":"https://evil.example","token":"ak_x"}'), 'simple-api')
  for (const t of ['', 'nope', '{"project":1}', '{"project":"../x"}', '{"project":"Has Space"}', 'null']) assert.equal(projectSlugFrom(t), undefined, t)
})

test('server URLs', () => {
  assert.equal(normalizeServerUrl('https://akili.example.com/'), 'https://akili.example.com')
  assert.equal(normalizeServerUrl(' https://example.com/akili/ '), 'https://example.com/akili')
  assert.equal(normalizeServerUrl('http://localhost:8080'), 'http://localhost:8080')
  for (const u of ['http://akili.example.com', 'ftp://x', 'https://u:p@x.example', 'https://x.example/?a=1', 'not a url', '']) assert.equal(normalizeServerUrl(u), undefined, u)
})

test('PKCE pairs match and are fresh', () => {
  const a = newSignInRequest()
  const b = newSignInRequest()
  assert.equal(challengeOf(a.verifier), a.challenge)
  assert.match(a.challenge, /^[A-Za-z0-9_-]{43}$/)
  assert.match(a.verifier, /^[A-Za-z0-9_-]{43}$/)
  assert.match(a.state, /^[A-Za-z0-9_-]{32}$/)
  assert.notEqual(a.verifier, b.verifier)
  assert.notEqual(a.state, b.state)
})
