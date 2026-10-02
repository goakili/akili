// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// A small, forgiving parser for unified diffs as `git diff` and forges produce them: `diff --git`
// headers, rename/copy/mode lines, ---/+++ paths, @@ hunks and binary files. Anything it does not
// recognise is ignored rather than thrown on, so a truncated or odd diff still renders what it can.

export type DiffLineKind = 'add' | 'del' | 'ctx' | 'meta'

export interface DiffLine {
  kind: DiffLineKind
  text: string
  /** Line number in the old file (del/ctx). */
  oldNo?: number
  /** Line number in the new file (add/ctx). */
  newNo?: number
}

export interface DiffHunk {
  header: string
  /** Text after the second @@ (often the enclosing function). */
  section: string
  oldStart: number
  newStart: number
  lines: DiffLine[]
}

export type FileStatus = 'modified' | 'added' | 'deleted' | 'renamed' | 'copied'

export interface DiffFile {
  oldPath: string
  newPath: string
  /** The path to show: the new path, or the old one for a deletion. */
  path: string
  status: FileStatus
  binary: boolean
  additions: number
  deletions: number
  hunks: DiffHunk[]
  /** Extra header lines worth showing (mode changes, similarity index). */
  notes: string[]
}

const HUNK_RE = /^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@ ?(.*)$/

/** Strips a/ b/ prefixes and quoting from a diff path; /dev/null stays as is. */
function cleanPath(p: string): string {
  let s = p.trim()
  // "--- a/file\t2024-01-01" (non-git diffs append a timestamp after a tab)
  const tab = s.indexOf('\t')
  if (tab >= 0) s = s.slice(0, tab)
  if (s.startsWith('"') && s.endsWith('"') && s.length >= 2) {
    s = s.slice(1, -1).replace(/\\(["\\])/g, '$1').replace(/\\t/g, '\t').replace(/\\n/g, '\n')
  }
  if (s === '/dev/null') return s
  if (s.startsWith('a/') || s.startsWith('b/')) s = s.slice(2)
  return s
}

/** Splits "diff --git a/x b/y" into the two paths (best effort when names contain spaces). */
function gitHeaderPaths(rest: string): [string, string] {
  const q = rest.match(/^"((?:\\.|[^"\\])*)" "((?:\\.|[^"\\])*)"$/)
  if (q) return [cleanPath(`"${q[1]}"`), cleanPath(`"${q[2]}"`)]
  // a/<p> b/<p>: with identical paths the split point is the middle.
  const half = (rest.length - 1) / 2
  if (Number.isInteger(half) && rest.charAt(half) === ' ') {
    const a = rest.slice(0, half)
    const b = rest.slice(half + 1)
    if (a.slice(2) === b.slice(2)) return [cleanPath(a), cleanPath(b)]
  }
  const i = rest.indexOf(' b/')
  if (i > 0) return [cleanPath(rest.slice(0, i)), cleanPath(rest.slice(i + 1))]
  const parts = rest.split(' ')
  return [cleanPath(parts[0] ?? ''), cleanPath(parts.slice(1).join(' '))]
}

function newFile(oldPath = '', newPath = ''): DiffFile {
  return { oldPath, newPath, path: newPath || oldPath, status: 'modified', binary: false, additions: 0, deletions: 0, hunks: [], notes: [] }
}

function finish(f: DiffFile): DiffFile {
  if (f.oldPath === '/dev/null') f.status = 'added'
  else if (f.newPath === '/dev/null') f.status = 'deleted'
  f.path = f.status === 'deleted' ? f.oldPath : f.newPath || f.oldPath
  return f
}

export function parseDiff(text: string): DiffFile[] {
  const files: DiffFile[] = []
  let file: DiffFile | null = null
  let hunk: DiffHunk | null = null
  let oldNo = 0
  let newNo = 0
  // Lines still expected in the current hunk; lets "--- x" inside a hunk count as a deletion.
  let oldLeft = 0
  let newLeft = 0

  const push = () => {
    if (file) files.push(finish(file))
    file = null
    hunk = null
    oldLeft = newLeft = 0
  }

  const lines = text.replace(/\r\n/g, '\n').split('\n')
  if (lines.length && lines[lines.length - 1] === '') lines.pop()

  for (const line of lines) {
    const inHunk = hunk !== null && (oldLeft > 0 || newLeft > 0)
    if (inHunk && file) {
      const h = hunk as DiffHunk
      const f = file as DiffFile
      const c = line.charAt(0)
      if (c === '+') {
        h.lines.push({ kind: 'add', text: line.slice(1), newNo: newNo++ })
        f.additions++
        newLeft--
        continue
      }
      if (c === '-') {
        h.lines.push({ kind: 'del', text: line.slice(1), oldNo: oldNo++ })
        f.deletions++
        oldLeft--
        continue
      }
      if (c === ' ' || line === '') {
        h.lines.push({ kind: 'ctx', text: line.slice(1), oldNo: oldNo++, newNo: newNo++ })
        oldLeft--
        newLeft--
        continue
      }
      if (c === '\\') {
        h.lines.push({ kind: 'meta', text: line.slice(1).trim() })
        continue
      }
      // Anything else ends the hunk early (malformed counts); fall through to header parsing.
      oldLeft = newLeft = 0
    }
    if (hunk && file && line.startsWith('\\')) {
      ;(hunk as DiffHunk).lines.push({ kind: 'meta', text: line.slice(1).trim() })
      continue
    }

    if (line.startsWith('diff --git ')) {
      push()
      const [a, b] = gitHeaderPaths(line.slice('diff --git '.length))
      file = newFile(a, b)
      continue
    }
    if (line.startsWith('diff ') && !line.startsWith('diff --git')) {
      // other diff flavours (diff -u, diff --cc): start a file, paths come from ---/+++
      push()
      file = newFile()
      continue
    }
    const hm = HUNK_RE.exec(line)
    if (hm) {
      if (!file) file = newFile()
      const f = file as DiffFile
      oldNo = Number(hm[1])
      newNo = Number(hm[3])
      oldLeft = hm[2] === undefined ? 1 : Number(hm[2])
      newLeft = hm[4] === undefined ? 1 : Number(hm[4])
      hunk = { header: line, section: (hm[5] ?? '').trim(), oldStart: oldNo, newStart: newNo, lines: [] }
      f.hunks.push(hunk)
      continue
    }
    if (line.startsWith('--- ')) {
      // A bare ---/+++ pair without a diff header starts a new file.
      if (!file || (file as DiffFile).hunks.length) {
        push()
        file = newFile()
      }
      ;(file as DiffFile).oldPath = cleanPath(line.slice(4))
      continue
    }
    if (line.startsWith('+++ ')) {
      if (!file) file = newFile()
      ;(file as DiffFile).newPath = cleanPath(line.slice(4))
      continue
    }
    if (!file) continue
    const f = file as DiffFile
    if (line.startsWith('new file mode')) {
      f.status = 'added'
      f.oldPath = '/dev/null'
    } else if (line.startsWith('deleted file mode')) {
      f.status = 'deleted'
      f.newPath = '/dev/null'
    } else if (line.startsWith('rename from ')) {
      f.status = 'renamed'
      f.oldPath = line.slice('rename from '.length)
    } else if (line.startsWith('rename to ')) {
      f.status = 'renamed'
      f.newPath = line.slice('rename to '.length)
    } else if (line.startsWith('copy from ')) {
      f.status = 'copied'
      f.oldPath = line.slice('copy from '.length)
    } else if (line.startsWith('copy to ')) {
      f.status = 'copied'
      f.newPath = line.slice('copy to '.length)
    } else if (line.startsWith('Binary files ') || line === 'GIT binary patch') {
      f.binary = true
      const m = line.match(/^Binary files (.+) and (.+) differ$/)
      if (m) {
        const a = cleanPath(m[1])
        const b = cleanPath(m[2])
        if (!f.oldPath) f.oldPath = a
        if (!f.newPath) f.newPath = b
        if (a === '/dev/null') f.oldPath = a
        if (b === '/dev/null') f.newPath = b
      }
    } else if (line.startsWith('old mode ') || line.startsWith('new mode ') || line.startsWith('similarity index ')) {
      f.notes.push(line)
    }
    // "index abc..def 100644" and other extended headers are ignored.
  }
  push()
  return files
}

export function diffTotals(files: DiffFile[]): { files: number; additions: number; deletions: number } {
  return files.reduce((t, f) => ({ files: t.files + 1, additions: t.additions + f.additions, deletions: t.deletions + f.deletions }), { files: 0, additions: 0, deletions: 0 })
}

/** Number of rendered lines of a file (for collapsing big ones). */
export function fileLineCount(f: DiffFile): number {
  return f.hunks.reduce((n, h) => n + h.lines.length + 1, 0)
}
