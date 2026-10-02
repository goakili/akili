<!-- SPDX-FileCopyrightText: 2026 Jonas Kaninda -->
<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
// Renders a unified diff: file list with +/- counts, collapsible files, hunks with line numbers.
// Every piece of diff text is rendered as a text node (never HTML). Large diffs stay responsive:
// files over `collapseOver` lines start collapsed, each file renders at most `lineCap` lines until
// "show more", and only the first FILE_CAP files render until asked.
import { computed, ref, watch } from 'vue'
import { parseDiff, diffTotals, fileLineCount, type DiffFile, type DiffHunk, type DiffLine, type FileStatus } from '../lib/diff'
import Icon, { type IconName } from './Icon'

const props = withDefaults(
  defineProps<{
    text: string
    /** Smaller chrome for tool cards: no file index, bounded height. */
    compact?: boolean
    collapseOver?: number
    lineCap?: number
  }>(),
  { compact: false, collapseOver: 400, lineCap: 800 },
)

const FILE_CAP = 60
const uid = `dv-${Math.random().toString(36).slice(2, 8)}`

const files = computed<DiffFile[]>(() => parseDiff(props.text ?? ''))
const totals = computed(() => diffTotals(files.value))

type Row = { t: 'hunk'; h: DiffHunk } | { t: 'line'; l: DiffLine }
const prepared = computed(() =>
  files.value.map((f) => {
    const rows: Row[] = []
    for (const h of f.hunks) {
      rows.push({ t: 'hunk', h })
      for (const l of h.lines) rows.push({ t: 'line', l })
    }
    return { f, rows, size: fileLineCount(f) }
  }),
)

const wrap = ref(false)
const collapsed = ref<Set<number>>(new Set())
const limits = ref<Map<number, number>>(new Map())
const fileCap = ref(FILE_CAP)

function reset() {
  const c = new Set<number>()
  prepared.value.forEach((p, i) => {
    if (p.size > props.collapseOver || p.f.binary) c.add(i)
  })
  // A compact view of many files starts with everything folded except the first.
  if (props.compact && prepared.value.length > 3) prepared.value.forEach((_, i) => i > 0 && c.add(i))
  collapsed.value = c
  limits.value = new Map()
  fileCap.value = FILE_CAP
}
watch(() => props.text, reset, { immediate: true })

function toggle(i: number) {
  const c = new Set(collapsed.value)
  if (c.has(i)) c.delete(i)
  else c.add(i)
  collapsed.value = c
}
const allCollapsed = computed(() => prepared.value.length > 0 && prepared.value.every((_, i) => collapsed.value.has(i)))
function setAll(fold: boolean) {
  collapsed.value = fold ? new Set(prepared.value.map((_, i) => i)) : new Set()
}

function limit(i: number) {
  return limits.value.get(i) ?? props.lineCap
}
function showMore(i: number) {
  const m = new Map(limits.value)
  m.set(i, limit(i) + 2000)
  limits.value = m
}

function jump(i: number) {
  if (collapsed.value.has(i)) toggle(i)
  if (i >= fileCap.value) fileCap.value = i + 1
  requestAnimationFrame(() => document.getElementById(`${uid}-f${i}`)?.scrollIntoView({ behavior: 'smooth', block: 'start' }))
}

const STATUS: Record<FileStatus, { letter: string; label: string; icon: IconName; tone: string }> = {
  added: { letter: 'A', label: 'added', icon: 'plus', tone: 'ok' },
  deleted: { letter: 'D', label: 'deleted', icon: 'trash', tone: 'danger' },
  modified: { letter: 'M', label: 'modified', icon: 'edit', tone: 'info' },
  renamed: { letter: 'R', label: 'renamed', icon: 'arrowRight', tone: 'violet' },
  copied: { letter: 'C', label: 'copied', icon: 'copy', tone: 'violet' },
}

/** Five-block change bar, like forges show. */
function blocks(f: DiffFile): ('add' | 'del' | 'none')[] {
  const total = f.additions + f.deletions
  if (!total) return ['none', 'none', 'none', 'none', 'none']
  // Filled blocks grow with the size of the change; the add/delete split follows the ratio.
  const n = Math.min(5, Math.max(1, Math.ceil(Math.log10(total + 1) * 2)))
  const addN = Math.round((f.additions / total) * n)
  const out: ('add' | 'del' | 'none')[] = []
  for (let i = 0; i < 5; i++) out.push(i >= n ? 'none' : i < addN ? 'add' : 'del')
  return out
}
</script>

<template>
  <div class="diffv" :class="{ compact, wrap }">
    <div class="dv-bar">
      <span class="dv-sum">
        <Icon name="gitDiff" />
        <strong>{{ totals.files }} {{ totals.files === 1 ? 'file' : 'files' }}</strong>
        <span class="dv-add num">+{{ totals.additions }}</span>
        <span class="dv-del num">−{{ totals.deletions }}</span>
      </span>
      <span class="grow" />
      <slot name="actions" />
      <button type="button" class="btn btn-xs btn-ghost" :aria-pressed="wrap" :title="wrap ? 'Do not wrap long lines' : 'Wrap long lines'" @click="wrap = !wrap">
        <Icon name="wrap" />{{ wrap ? 'Wrapped' : 'Wrap' }}
      </button>
      <button v-if="files.length > 1" type="button" class="btn btn-xs btn-ghost" @click="setAll(!allCollapsed)">
        <Icon :name="allCollapsed ? 'chevronDown' : 'chevronRight'" />{{ allCollapsed ? 'Expand all' : 'Collapse all' }}
      </button>
    </div>

    <p v-if="!files.length" class="dv-empty">No file changes in this diff.</p>

    <nav v-else-if="!compact && files.length > 1" class="dv-index" aria-label="Changed files">
      <button v-for="(p, i) in prepared" :key="i" type="button" class="dv-index-row" @click="jump(i)">
        <span class="dv-letter" :class="STATUS[p.f.status].tone" :title="STATUS[p.f.status].label">{{ STATUS[p.f.status].letter }}</span>
        <span class="dv-path truncate mono">{{ p.f.path }}</span>
        <span v-if="p.f.binary" class="xs muted">binary</span>
        <span v-else class="dv-counts num"><span class="dv-add">+{{ p.f.additions }}</span> <span class="dv-del">−{{ p.f.deletions }}</span></span>
        <span class="dv-blocks" aria-hidden="true"><i v-for="(b, k) in blocks(p.f)" :key="k" :class="b" /></span>
      </button>
    </nav>

    <div class="dv-files">
      <section v-for="(p, i) in prepared.slice(0, fileCap)" :id="`${uid}-f${i}`" :key="i" class="dv-file">
        <button type="button" class="dv-fhead" :aria-expanded="!collapsed.has(i)" :aria-controls="`${uid}-b${i}`" @click="toggle(i)">
          <Icon name="chevronRight" class="chev" />
          <span class="badge square" :class="STATUS[p.f.status].tone"><Icon :name="STATUS[p.f.status].icon" />{{ STATUS[p.f.status].label }}</span>
          <span class="dv-path mono">
            <template v-if="p.f.status === 'renamed' || p.f.status === 'copied'"><span class="muted">{{ p.f.oldPath }} → </span></template>{{ p.f.path }}
          </span>
          <span class="grow" />
          <span v-if="p.f.binary" class="xs muted">binary file</span>
          <span v-else class="dv-counts num"><span class="dv-add">+{{ p.f.additions }}</span> <span class="dv-del">−{{ p.f.deletions }}</span></span>
        </button>
        <div v-if="!collapsed.has(i)" :id="`${uid}-b${i}`" class="dv-body">
          <div v-for="n in p.f.notes" :key="n" class="dv-note mono">{{ n }}</div>
          <div v-if="p.f.binary" class="dv-note">Binary file; contents are not shown.</div>
          <div v-else-if="!p.f.hunks.length" class="dv-note">{{ p.f.status === 'renamed' ? 'Renamed without content changes.' : 'No content changes.' }}</div>
          <div v-else class="dv-scroll">
            <table class="dv-table">
              <colgroup><col class="c-ln" /><col class="c-ln" /><col /></colgroup>
              <tbody>
                <template v-for="(r, k) in p.rows.slice(0, limit(i))" :key="k">
                  <tr v-if="r.t === 'hunk'" class="dv-hunk">
                    <td class="ln" colspan="2" aria-hidden="true">⋯</td>
                    <td class="dv-code">{{ r.h.header }}</td>
                  </tr>
                  <tr v-else :class="`r-${r.l.kind}`">
                    <td class="ln">{{ r.l.oldNo ?? '' }}</td>
                    <td class="ln">{{ r.l.newNo ?? '' }}</td>
                    <td class="dv-code"><span class="sign" aria-hidden="true">{{ r.l.kind === 'add' ? '+' : r.l.kind === 'del' ? '-' : r.l.kind === 'meta' ? '\\' : ' ' }}</span><span v-if="r.l.kind === 'add'" class="sr-only">added: </span><span v-else-if="r.l.kind === 'del'" class="sr-only">removed: </span>{{ r.l.text }}</td>
                  </tr>
                </template>
              </tbody>
            </table>
          </div>
          <button v-if="p.rows.length > limit(i)" type="button" class="dv-more" @click="showMore(i)">
            <Icon name="chevronDown" />Show {{ Math.min(2000, p.rows.length - limit(i)) }} more lines ({{ p.rows.length - limit(i) }} hidden)
          </button>
        </div>
        <div v-else-if="p.size > collapseOver" class="dv-note folded">
          Large change ({{ p.size }} lines) folded. <button type="button" class="btn-link" @click="toggle(i)">Show it</button>
        </div>
      </section>
      <button v-if="prepared.length > fileCap" type="button" class="dv-more" @click="fileCap = prepared.length">
        <Icon name="chevronDown" />Show {{ prepared.length - fileCap }} more files
      </button>
    </div>
  </div>
</template>

<style scoped>
.diffv {
  --dv-add-bg: color-mix(in srgb, var(--success-500) 13%, var(--bg-primary));
  --dv-add-ln: color-mix(in srgb, var(--success-500) 22%, var(--bg-primary));
  --dv-del-bg: color-mix(in srgb, var(--danger-500) 12%, var(--bg-primary));
  --dv-del-ln: color-mix(in srgb, var(--danger-500) 21%, var(--bg-primary));
  --dv-hunk-bg: color-mix(in srgb, var(--info-500) 9%, var(--bg-primary));
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-width: 0;
}
.dv-bar {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}
.dv-sum {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}
.dv-sum .icon {
  width: 16px;
  height: 16px;
  color: var(--text-tertiary);
}
.dv-add {
  color: var(--success-text);
  font-weight: 600;
}
.dv-del {
  color: var(--danger-text);
  font-weight: 600;
}
.dv-empty {
  margin: 0;
  color: var(--text-tertiary);
  font-size: 13px;
}
.dv-index {
  border: 1px solid var(--border-primary);
  border-radius: var(--radius);
  overflow: hidden;
  max-height: 260px;
  overflow-y: auto;
}
.dv-index-row {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 7px 12px;
  border: none;
  border-bottom: 1px solid var(--border-secondary);
  background: var(--bg-primary);
  color: var(--text-primary);
  font: inherit;
  font-size: 12.5px;
  text-align: left;
  cursor: pointer;
}
.dv-index-row:last-child {
  border-bottom: none;
}
.dv-index-row:hover {
  background: var(--bg-hover);
}
.dv-letter {
  flex: none;
  width: 18px;
  height: 18px;
  border-radius: 4px;
  display: inline-grid;
  place-items: center;
  font-size: 11px;
  font-weight: 700;
  background: var(--bg-tertiary);
  color: var(--text-secondary);
}
.dv-letter.ok {
  background: var(--success-50);
  color: var(--success-text);
}
.dv-letter.danger {
  background: var(--danger-50);
  color: var(--danger-text);
}
.dv-letter.info {
  background: var(--info-50);
  color: var(--info-text);
}
.dv-letter.violet {
  background: var(--violet-50);
  color: var(--violet-text);
}
.dv-path {
  min-width: 0;
  flex: 1;
  font-size: 12.5px;
  overflow-wrap: anywhere;
}
.dv-counts {
  flex: none;
  font-size: 12px;
  font-family: var(--mono);
}
.dv-blocks {
  display: inline-flex;
  gap: 2px;
  flex: none;
}
.dv-blocks i {
  width: 8px;
  height: 8px;
  border-radius: 2px;
  background: var(--border-primary);
}
.dv-blocks i.add {
  background: var(--success-500);
}
.dv-blocks i.del {
  background: var(--danger-500);
}
.dv-files {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.dv-file {
  border: 1px solid var(--border-primary);
  border-radius: var(--radius);
  overflow: hidden;
  background: var(--bg-primary);
  scroll-margin-top: calc(var(--topbar-h) + 12px);
}
.dv-fhead {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 8px 12px;
  border: none;
  background: var(--bg-secondary);
  color: var(--text-primary);
  font: inherit;
  text-align: left;
  cursor: pointer;
  position: sticky;
  top: 0;
  z-index: 1;
}
.dv-fhead:hover {
  background: var(--bg-hover);
}
.dv-fhead .dv-path {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  overflow-wrap: normal;
}
.dv-fhead .badge {
  flex: none;
}
.dv-fhead .chev {
  width: 14px;
  height: 14px;
  flex: none;
  color: var(--text-tertiary);
  transition: transform var(--t) var(--ease);
}
.dv-fhead[aria-expanded='true'] .chev {
  transform: rotate(90deg);
}
.dv-fhead[aria-expanded='true'] {
  border-bottom: 1px solid var(--border-primary);
}
.dv-note {
  padding: 8px 12px;
  font-size: 12.5px;
  color: var(--text-tertiary);
  border-bottom: 1px solid var(--border-secondary);
}
.dv-note.folded {
  border-bottom: none;
  border-top: 1px solid var(--border-primary);
}
.dv-scroll {
  overflow-x: auto;
}
.dv-table {
  width: 100%;
  border-collapse: collapse;
  font-family: var(--mono);
  font-size: 12px;
  line-height: 1.55;
  color: var(--text-primary);
  table-layout: auto;
}
.wrap .dv-table {
  table-layout: fixed;
}
.c-ln {
  width: 1%;
}
.wrap .c-ln {
  width: 52px;
}
.dv-table td {
  padding: 0 10px;
  vertical-align: top;
  border: none;
}
.dv-table td.ln {
  min-width: 44px;
  text-align: right;
  color: var(--text-muted);
  user-select: none;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
  border-right: 1px solid var(--border-secondary);
  padding: 0 8px;
}
.dv-table td.dv-code {
  white-space: pre;
  width: 100%;
}
.wrap .dv-table td.dv-code {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.sign {
  display: inline-block;
  width: 1.4ch;
  color: var(--text-muted);
  user-select: none;
}
tr.r-add td {
  background: var(--dv-add-bg);
}
tr.r-add td.ln {
  background: var(--dv-add-ln);
  color: var(--success-text);
}
tr.r-add .sign {
  color: var(--success-text);
}
tr.r-del td {
  background: var(--dv-del-bg);
}
tr.r-del td.ln {
  background: var(--dv-del-ln);
  color: var(--danger-text);
}
tr.r-del .sign {
  color: var(--danger-text);
}
tr.r-meta td.dv-code {
  color: var(--text-tertiary);
  font-style: italic;
}
tr.dv-hunk td {
  background: var(--dv-hunk-bg);
  color: var(--info-text);
  padding-top: 3px;
  padding-bottom: 3px;
}
tr.dv-hunk td.ln {
  text-align: center;
}
.dv-more {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  width: 100%;
  padding: 8px;
  border: none;
  border-top: 1px solid var(--border-primary);
  background: var(--bg-secondary);
  color: var(--primary-text);
  font: inherit;
  font-size: 12.5px;
  font-weight: 550;
  cursor: pointer;
}
.dv-files > .dv-more {
  border: 1px dashed var(--border-input);
  border-radius: var(--radius);
}
.dv-more:hover {
  background: var(--bg-hover);
}
.dv-more .icon {
  width: 14px;
  height: 14px;
}
/* compact (tool cards) */
.compact {
  gap: 8px;
}
.compact .dv-files {
  max-height: 420px;
  overflow: auto;
  gap: 8px;
}
.compact .dv-fhead {
  padding: 6px 10px;
}
.compact .dv-table {
  font-size: 11.5px;
}
@media (max-width: 640px) {
  .dv-table td.ln {
    min-width: 32px;
    padding: 0 5px;
  }
  .dv-blocks {
    display: none;
  }
}
</style>
