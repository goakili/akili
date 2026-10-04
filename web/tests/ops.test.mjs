// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Unit tests for change-plan helpers and the asciinema cast parser (`npm test`).
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { parsePlan, callSummary, checkFailed, callStatus, mcpTool, miabiState, toolGroup, toolIcon, truncate } from '../src/lib/tools.ts'
import { parseCast, inputLines, visibleKeys } from '../src/lib/cast.ts'

test('parsePlan reads a change_run input, object or JSON text', () => {
  const input = { title: 'Free cache', reason: 'disk full', steps: [{ tool: 'shell', input: { command: 'rm -f x' } }], verify: [{ tool: 'fs_list', input: { path: 'c' }, expect: 'keep' }] }
  const p = parsePlan(input)
  assert.equal(p.title, 'Free cache')
  assert.equal(p.steps.length, 1)
  assert.equal(p.verify[0].expect, 'keep')
  assert.deepEqual(p.rollback, [])
  assert.equal(parsePlan(JSON.stringify(input)).title, 'Free cache')
  assert.equal(parsePlan({ command: 'ls' }), null)
  assert.equal(parsePlan('not json'), null)
})

test('callSummary prefers the argument that says what a call touches', () => {
  assert.equal(callSummary({ unit: 'nginx', lines: 50 }), 'nginx')
  assert.equal(callSummary({ host: 'db', port: 5432 }), 'db:5432')
  assert.equal(callSummary(null), '')
})

test('callSummary names the Miabi app and what changes', () => {
  assert.equal(callSummary({ integration: 'prod', app: 'api', tag: 'v4' }, 'miabi_deploy'), 'api → v4')
  assert.equal(callSummary({ app: 'api' }, 'miabi_rollback'), 'api')
  assert.equal(callSummary({ app: 'api', release_id: 12 }, 'miabi_rollback'), 'api → release 12')
  assert.equal(callSummary({ app: 'api', deployment_id: 'd-7' }, 'miabi_deploy_logs'), 'api · deployment d-7')
  assert.equal(callSummary({ app: 'api', lines: 200 }, 'miabi_logs'), 'api')
  assert.equal(callSummary({ app: 'api', tag: 'v4' }), 'api → v4')
  assert.equal(callSummary({ unit: 'nginx' }, 'service_status'), 'nginx')
})

test('Miabi tools have icons, a group and a parsed state', () => {
  assert.equal(toolIcon('miabi_rollback'), 'undo')
  assert.equal(toolGroup('miabi_status'), 'Miabi')
  assert.notEqual(toolGroup('shell'), 'Miabi')
  assert.deepEqual(miabiState('app: api\nstatus: Running\nhealth: unhealthy\n'), { status: 'running', health: 'unhealthy', traffic: '', maintenance: '' })
  assert.deepEqual(miabiState('status: running'), { status: 'running', health: '', traffic: '', maintenance: '' })
  assert.equal(miabiState('no state here'), null)
  assert.equal(miabiState(''), null)
})

test('Miabi summaries name workspace/app and what changes', () => {
  assert.equal(callSummary({ workspace: 'prod', app: 'api', tag: 'v4' }, 'miabi_deploy'), 'prod/api → v4')
  assert.equal(callSummary({ workspace: 'staging', app: 'api', replicas: 3 }, 'miabi_scale'), 'staging/api ×3 replicas')
  assert.equal(callSummary({ workspace: 'staging', app: 'api', replicas: 0 }, 'miabi_scale'), 'staging/api ×0 replicas')
  assert.equal(callSummary({ workspace: 'prod', database: 'pg-main', backup: 3 }, 'miabi_db_restore'), 'prod/db pg-main backup 3')
  assert.equal(callSummary({ workspace: 'prod', database: 'pg-main', db: 'shop' }, 'miabi_db_backup'), 'prod/db pg-main/shop')
  assert.equal(callSummary({ workspace: 'staging', app: 'api', enabled: true, message: 'brb' }, 'miabi_maintenance'), 'staging/api maintenance on')
  assert.equal(callSummary({ workspace: 'prod', app: 'api', action: 'abort' }, 'miabi_canary'), 'prod/api canary abort')
  assert.equal(callSummary({ workspace: 'prod', app: 'api', key: 'DB_URL', value: 'postgres://secret' }, 'miabi_env_set'), 'prod/api set DB_URL')
  assert.equal(callSummary({ workspace: 'prod', name: 'build', branch: 'main' }, 'miabi_pipeline_run'), 'prod/pipeline build @ main')
  assert.equal(callSummary({ workspace: 'prod', name: 'nightly' }, 'miabi_cron_run'), 'prod/cron nightly')
  assert.equal(callSummary({ workspace: 'prod', alert: 42, action: 'ack' }, 'miabi_alert'), 'prod · alert 42 → ack')
  assert.equal(callSummary({ workspace: 'prod' }, 'miabi_overview'), 'prod')
  assert.equal(toolIcon('miabi_db_backup'), 'package')
  assert.equal(toolGroup('miabi_traffic'), 'Miabi')
})

test('miabiState reads traffic and maintenance lines', () => {
  assert.deepEqual(miabiState('app: api\ntraffic: Degraded\nmaintenance: on\n'), { status: '', health: '', traffic: 'degraded', maintenance: 'on' })
  assert.equal(miabiState('traffic: no-data').traffic, 'no-data')
})

test('MCP tools get a server group and a summary of their first arguments', () => {
  assert.deepEqual(mcpTool('mcp__miabi__list_apps'), { server: 'miabi', tool: 'list_apps' })
  assert.deepEqual(mcpTool('mcp__my-srv__get__thing'), { server: 'my-srv', tool: 'get__thing' })
  assert.equal(mcpTool('miabi_apps'), null)
  assert.equal(toolGroup('mcp__miabi__list_apps'), 'MCP · miabi')
  assert.equal(toolIcon('mcp__miabi__list_apps'), 'plug')
  assert.equal(callSummary({ workspace: 'prod', app: 'api', lines: 50 }, 'mcp__miabi__logs'), 'prod · api')
  assert.equal(callSummary({ filter: { a: 1 }, limit: 5 }, 'mcp__x__q'), '5')
})

test('plan_propose shows the plan title and its phase count', () => {
  assert.equal(toolIcon('plan_propose'), 'list')
  assert.equal(callSummary({ title: 'Version  endpoint', phases: [{ title: 'a' }, { title: 'b' }] }, 'plan_propose'), 'Version endpoint · 2 phases')
  assert.equal(callSummary({ title: 'One', phases: [{ title: 'a' }] }, 'plan_propose'), 'One · 1 phase')
  assert.equal(callSummary({}, 'plan_propose'), '')
})

test('lesson_propose shows the lesson text, truncated', () => {
  assert.equal(toolIcon('lesson_propose'), 'lightbulb')
  assert.equal(callSummary({ lesson: 'Deploys  to prod\nneed a tag.' }, 'lesson_propose'), 'Deploys to prod need a tag.')
  const long = callSummary({ lesson: 'x'.repeat(200) }, 'lesson_propose')
  assert.equal(long.length, 80)
  assert.ok(long.endsWith('…'))
  assert.equal(callSummary({}, 'lesson_propose'), '')
  assert.equal(truncate('short', 10), 'short')
})

test('verify checks are re-derived from expect/reject', () => {
  const base = { phase: 'verify', tool: 'fs_list', input: {}, hash: '', risk: 'low', status: 'ok' }
  assert.equal(checkFailed({ ...base, expect: 'v=3', output: 'v=2' }), 'output did not contain “v=3”')
  assert.equal(checkFailed({ ...base, reject: 'big.bin', output: 'big.bin keep.txt' }), 'output contained “big.bin”')
  assert.equal(checkFailed({ ...base, expect: 'keep', reject: 'big', output: 'keep.txt' }), '')
  assert.equal(callStatus({ ...base, expect: 'x', output: 'y' }), 'check_failed')
  assert.equal(callStatus({ ...base, phase: 'step', expect: 'x', output: 'y' }), 'ok')
})

test('parseCast reads asciinema v2 and shortens idle gaps', () => {
  const cast = [
    JSON.stringify({ version: 2, width: 100, height: 30 }),
    JSON.stringify([0.5, 'o', '$ ']),
    JSON.stringify([1.0, 'i', 'e']),
    JSON.stringify([1.1, 'i', 'c']),
    JSON.stringify([1.2, 'i', '\r']),
    JSON.stringify([30.0, 'o', 'hi\r\n']),
    '[31.0, "o", "trunc',
  ].join('\n')
  const c = parseCast(cast)
  assert.equal(c.header.width, 100)
  assert.equal(c.events.length, 5)
  assert.equal(c.compressed, 1)
  assert.ok(Math.abs(c.duration - 3.2) < 1e-9)
  assert.deepEqual(inputLines(c.events).map((l) => l.text), ['ec⏎'])
  assert.equal(visibleKeys('\x03\x7f'), '^C⌫')
  assert.throws(() => parseCast('{"version":1}'))
})

test('safeNavUrl allows same-origin paths and http(s) URLs only', async () => {
  const { safeNavUrl } = await import('../src/lib/format.ts')
  assert.equal(safeNavUrl('/api/v1/auth/sso/login'), '/api/v1/auth/sso/login')
  assert.equal(safeNavUrl('https://idp.example.com/authorize?x=1'), 'https://idp.example.com/authorize?x=1')
  assert.equal(safeNavUrl('//evil.example.com'), undefined)
  assert.equal(safeNavUrl('/\\evil.example.com'), undefined)
  assert.equal(safeNavUrl('javascript:alert(1)'), undefined)
  assert.equal(safeNavUrl(''), undefined)
  assert.equal(safeNavUrl(undefined), undefined)
})
