// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

import { ref, type Ref } from 'vue'
import type { Page } from '../api'

type Key = string | number

export interface Paged<T> {
  items: Ref<T[]>
  /** The first page is loading. */
  loading: Ref<boolean>
  /** A later page is loading. */
  loadingMore: Ref<boolean>
  hasMore: Ref<boolean>
  /** Rows matching the query, while the server reports it. */
  total: Ref<number | null>
  /** Load the first page again (after a filter change or a reconnect). */
  reload: () => Promise<void>
  /** Re-read the first page and put it on top, keeping the pages already scrolled through. */
  refresh: () => Promise<void>
  /** Append the next page. */
  more: () => Promise<void>
  /** Replace an item in place, or insert it at the top. */
  upsert: (item: T) => void
  remove: (key: Key) => void
}

/**
 * A list loaded page by page for infinite scroll. Rows inserted on the server between two page loads
 * shift offsets, so a row can arrive twice; it is kept once, by key.
 */
export function usePaged<T>(fetchPage: (page: number) => Promise<Page<T>>, key: (t: T) => Key = (t) => (t as { id: Key }).id): Paged<T> {
  const items = ref<T[]>([]) as Ref<T[]>
  const loading = ref(true)
  const loadingMore = ref(false)
  const hasMore = ref(false)
  const total = ref<number | null>(null)
  let next: number | null = null
  // A reload that starts while a page is in flight wins; the stale page is dropped.
  let gen = 0

  async function reload() {
    const g = ++gen
    loading.value = true
    loadingMore.value = false
    try {
      const p = await fetchPage(0)
      if (g !== gen) return
      items.value = dedupe(p.items)
      next = p.next
      hasMore.value = p.next !== null
      total.value = p.total
    } catch {
      if (g === gen) hasMore.value = false
    } finally {
      if (g === gen) loading.value = false
    }
  }

  async function refresh() {
    if (loading.value) return reload()
    const g = gen
    try {
      const p = await fetchPage(0)
      if (g !== gen) return
      const head = dedupe(p.items)
      const seen = new Set(head.map(key))
      items.value = head.concat(items.value.filter((t) => !seen.has(key(t))))
      if (p.total !== null) total.value = p.total
      // Only the first page was read; an already-exhausted list stays exhausted unless it grew past it.
      if (!hasMore.value && p.next !== null && next === null) {
        next = p.next
        hasMore.value = true
      }
    } catch {
      /* keep what is shown */
    }
  }

  async function more() {
    if (next === null || loading.value || loadingMore.value) return
    const g = gen
    loadingMore.value = true
    try {
      const p = await fetchPage(next)
      if (g !== gen) return
      const seen = new Set(items.value.map(key))
      items.value = items.value.concat(p.items.filter((t) => !seen.has(key(t))))
      next = p.next
      hasMore.value = p.next !== null
      if (p.total !== null) total.value = p.total
    } catch {
      // Stop on an error (already toasted) instead of retrying in a loop while the sentinel is visible.
      if (g === gen) hasMore.value = false
    } finally {
      if (g === gen) loadingMore.value = false
    }
  }

  function dedupe(list: T[]): T[] {
    const seen = new Set<Key>()
    return list.filter((t) => !seen.has(key(t)) && !!seen.add(key(t)))
  }

  function upsert(item: T) {
    const k = key(item)
    const i = items.value.findIndex((x) => key(x) === k)
    if (i >= 0) items.value[i] = item
    else items.value.unshift(item)
  }

  function remove(k: Key) {
    items.value = items.value.filter((x) => key(x) !== k)
  }

  return { items, loading, loadingMore, hasMore, total, reload, refresh, more, upsert, remove }
}

/** Load every page of a list that is small by nature (open tasks, pending approvals), up to a cap. */
export async function fetchAll<T>(fetchPage: (page: number) => Promise<Page<T>>, max = 1000): Promise<T[]> {
  const out: T[] = []
  for (let page: number | null = 0; page !== null && out.length < max; ) {
    const p: Page<T> = await fetchPage(page)
    out.push(...p.items)
    page = p.next
  }
  return out
}

/** Load pages until `stop` holds for a row (lists are newest first), e.g. rows older than a date. */
export async function fetchUntil<T>(fetchPage: (page: number) => Promise<Page<T>>, stop: (t: T) => boolean, max = 1000): Promise<T[]> {
  const out: T[] = []
  for (let page: number | null = 0; page !== null && out.length < max; ) {
    const p: Page<T> = await fetchPage(page)
    const cut = p.items.findIndex(stop)
    if (cut >= 0) return out.concat(p.items.slice(0, cut))
    out.push(...p.items)
    page = p.next
  }
  return out
}
