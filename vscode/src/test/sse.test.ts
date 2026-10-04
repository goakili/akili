// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

import { test } from 'node:test'
import * as assert from 'node:assert/strict'
import { SSEParser } from '../sse'

test('events split across chunks and line endings', () => {
  const got: string[] = []
  const p = new SSEParser((d) => got.push(d))
  p.push('data: {"type":"rea')
  p.push('dy"}\n\n: keep-alive\r\n\r\ndata:a\r\ndata: b\r\n\r')
  p.push('\nevent: x\nid: 1\ndata: c\n\n')
  assert.deepEqual(got, ['{"type":"ready"}', 'a\nb', 'c'])
})

test('a blank line without data emits nothing', () => {
  const got: string[] = []
  const p = new SSEParser((d) => got.push(d))
  p.push('\n\nretry: 10\n\n')
  assert.deepEqual(got, [])
})
