// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Unit tests for which events play a sound. Runs on Node's built-in test runner: `npm test`.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { soundFor, parseSoundPrefs, DEFAULT_SOUND_PREFS } from '../src/lib/sounds.ts'

const NOW = '2026-10-02T10:00:30Z'
const ctx = (over = {}) => ({ userId: 'usr_me', canApprove: true, prefs: { ...DEFAULT_SOUND_PREFS }, announced: new Set(), ...over })
const task = (data, ts = NOW) => ({
  type: 'task.updated',
  ts,
  data: { id: 'tsk_1', status: 'succeeded', created_by: 'usr_me', attempts: 1, finished_at: '2026-10-02T10:00:29Z', ...data },
})

test('approval requests chime only for people who can approve', () => {
  const ev = { type: 'approval.created', ts: NOW }
  assert.equal(soundFor(ev, ctx()), 'approval')
  assert.equal(soundFor(ev, ctx({ canApprove: false })), null)
  assert.equal(soundFor(ev, ctx({ prefs: { ...DEFAULT_SOUND_PREFS, approvals: false } })), null)
})

test('my finished tasks chime by outcome; running and cancelled ones do not', () => {
  assert.equal(soundFor(task({}), ctx()), 'success')
  assert.equal(soundFor(task({ status: 'failed' }), ctx()), 'failure')
  assert.equal(soundFor(task({ status: 'timed_out' }), ctx()), 'failure')
  assert.equal(soundFor(task({ status: 'running', finished_at: null }), ctx()), null)
  assert.equal(soundFor(task({ status: 'cancelled' }), ctx()), null)
})

test("other people's and triggered tasks need 'any task'", () => {
  const theirs = task({ created_by: 'usr_other' })
  const triggered = task({ created_by: '' })
  assert.equal(soundFor(theirs, ctx()), null)
  assert.equal(soundFor(triggered, ctx()), null)
  const all = { ...DEFAULT_SOUND_PREFS, allTasks: true }
  assert.equal(soundFor(theirs, ctx({ prefs: all })), 'success')
  assert.equal(soundFor(triggered, ctx({ prefs: all })), 'success')
  assert.equal(soundFor(task({}), ctx({ userId: null })), null)
})

test('each outcome chimes once; a retry that fails again chimes again', () => {
  const c = ctx()
  assert.equal(soundFor(task({ status: 'failed' }), c), 'failure')
  assert.equal(soundFor(task({ status: 'failed' }), c), null)
  assert.equal(soundFor(task({ status: 'failed', attempts: 2 }), c), 'failure')
})

test('a long-finished task updated again stays silent', () => {
  assert.equal(soundFor(task({ finished_at: '2026-10-02T09:00:00Z' }), ctx()), null)
})

test('muted means silent, and other events never play', () => {
  assert.equal(soundFor(task({}), ctx({ prefs: { ...DEFAULT_SOUND_PREFS, enabled: false } })), null)
  assert.equal(soundFor({ type: 'approval.created', ts: NOW }, ctx({ prefs: { ...DEFAULT_SOUND_PREFS, enabled: false } })), null)
  for (const type of ['approval.resolved', 'session.state', 'agent.status', 'change.updated']) {
    assert.equal(soundFor({ type, ts: NOW, data: { status: 'succeeded' } }, ctx({ prefs: { ...DEFAULT_SOUND_PREFS, allTasks: true } })), null)
  }
})

test('stored preferences fall back to defaults when missing or malformed', () => {
  assert.deepEqual(parseSoundPrefs(null), DEFAULT_SOUND_PREFS)
  assert.deepEqual(parseSoundPrefs('not json'), DEFAULT_SOUND_PREFS)
  const p = parseSoundPrefs(JSON.stringify({ enabled: false, volume: 7, allTasks: 'yes', myTasks: false }))
  assert.equal(p.enabled, false)
  assert.equal(p.volume, 1)
  assert.equal(p.allTasks, false)
  assert.equal(p.myTasks, false)
  assert.equal(p.approvals, true)
})
