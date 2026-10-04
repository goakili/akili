// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

import { test } from 'node:test'
import * as assert from 'node:assert/strict'
import { allowedCall, allowedStream } from '../proxy'

test('the sidebar calls are allowed', () => {
  for (const [m, p] of [
    ['GET', '/api/v1/auth/me'],
    ['GET', '/api/v1/sessions?project_id=prj_1&mode=chat&limit=30'],
    ['GET', '/api/v1/sessions/ses_1'],
    ['POST', '/api/v1/sessions'],
    ['POST', '/api/v1/sessions/ses_1/messages'],
    ['POST', '/api/v1/approvals/apr_1/approve'],
    ['GET', '/api/v1/tasks/tsk_1/diff'],
    ['POST', '/api/v1/tasks'],
  ]) {
    assert.ok(allowedCall(m, p), `${m} ${p}`)
  }
})

test('everything else is refused', () => {
  for (const [m, p] of [
    ['POST', '/api/v1/api-keys'],
    ['GET', '/api/v1/api-keys'],
    ['GET', '/api/v1/users'],
    ['PUT', '/api/v1/policies/pol_1'],
    ['POST', '/api/v1/auth/vscode/authorize'],
    ['DELETE', '/api/v1/sessions/ses_1'],
    ['GET', '/api/v1/agents/ag_1/terminal'],
    ['GET', '/api/v1/sessions/../users'],
    ['GET', '/api/v1/sessions/%2e%2e/users'],
    ['GET', '/api/v1//users'],
    ['GET', 'https://evil.example/api/v1/auth/me'],
    ['GET', '/api/v1/sessions/ses_1#x'],
    ['POST', '/api/v1/approvals/apr_1/approve/extra'],
  ]) {
    assert.ok(!allowedCall(m, p), `${m} ${p}`)
  }
})

test('only the event stream can be opened', () => {
  assert.ok(allowedStream('/api/v1/events/stream'))
  assert.ok(allowedStream('/api/v1/events/stream?session=ses_1'))
  assert.ok(!allowedStream('/api/v1/events/stream?session=ses_1&x=y'))
  assert.ok(!allowedStream('/api/v1/sessions/ses_1/stream'))
  assert.ok(!allowedStream('/api/v1/agents/ag_1/terminal'))
})
