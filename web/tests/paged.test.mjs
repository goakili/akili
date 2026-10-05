// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Unit tests for the infinite-scroll list helpers: `npm test`.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { fetchAll, fetchUntil, usePaged } from '../src/lib/paged.ts'

/** A server list of `rows` (newest first) served in pages of `size`, like the API. */
function server(rows, size = 2) {
  return async (page) => {
    const items = rows.slice(page * size, (page + 1) * size)
    const more = (page + 1) * size < rows.length
    return { items, next: more ? page + 1 : null, total: more ? rows.length : null }
  }
}
const ids = (list) => list.map((r) => r.id)

test('more appends pages until the server has no next page', async () => {
  const p = usePaged(server([{ id: 'e' }, { id: 'd' }, { id: 'c' }, { id: 'b' }, { id: 'a' }]))
  await p.reload()
  assert.deepEqual(ids(p.items.value), ['e', 'd'])
  assert.equal(p.hasMore.value, true)
  assert.equal(p.total.value, 5)
  await p.more()
  await p.more()
  assert.deepEqual(ids(p.items.value), ['e', 'd', 'c', 'b', 'a'])
  assert.equal(p.hasMore.value, false)
  await p.more()
  assert.equal(p.items.value.length, 5, 'more on the last page is a no-op')
})

test('a row shifted onto the next page by a new insert is kept once', async () => {
  const rows = [{ id: 'c' }, { id: 'b' }, { id: 'a' }]
  const p = usePaged(server(rows))
  await p.reload()
  rows.unshift({ id: 'new' })
  await p.more()
  assert.deepEqual(ids(p.items.value), ['c', 'b', 'a'])
})

test('a page that arrives after a reload is dropped', async () => {
  let release
  const slow = new Promise((r) => (release = r))
  let filter = 'old'
  const p = usePaged(async (page) => {
    if (filter === 'old' && page === 1) await slow
    return { items: [{ id: `${filter}-${page}` }], next: page === 0 ? 1 : null, total: null }
  })
  await p.reload()
  const pending = p.more()
  filter = 'new'
  await p.reload()
  release()
  await pending
  assert.deepEqual(ids(p.items.value), ['new-0'])
})

test('refresh puts the first page on top and keeps the pages scrolled through', async () => {
  const rows = [{ id: 'd' }, { id: 'c' }, { id: 'b' }, { id: 'a' }]
  const p = usePaged(server(rows))
  await p.reload()
  await p.more()
  rows.splice(2, 1)
  rows.unshift({ id: 'b', v: 2 })
  await p.refresh()
  assert.deepEqual(ids(p.items.value), ['b', 'd', 'c', 'a'])
  assert.equal(p.items.value[0].v, 2)
})

test('upsert replaces in place or inserts at the top; remove drops by key', async () => {
  const p = usePaged(server([{ id: 'b' }, { id: 'a' }]))
  await p.reload()
  p.upsert({ id: 'a', v: 1 })
  p.upsert({ id: 'c' })
  p.remove('b')
  assert.deepEqual(p.items.value, [{ id: 'c' }, { id: 'a', v: 1 }])
})

test('fetchAll reads every page; fetchUntil stops at the first row that matches', async () => {
  const rows = [5, 4, 3, 2, 1].map((n) => ({ id: String(n), n }))
  assert.deepEqual(ids(await fetchAll(server(rows))), ['5', '4', '3', '2', '1'])
  assert.deepEqual(ids(await fetchUntil(server(rows), (r) => r.n < 3)), ['5', '4', '3'])
  assert.equal((await fetchAll(server(rows), 3)).length, 4, 'stops after the page that reaches the cap')
})
