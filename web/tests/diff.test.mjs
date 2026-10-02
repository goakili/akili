// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Unit tests for the unified-diff parser. Runs on Node's built-in test runner (Node >= 23 strips
// TypeScript types natively): `npm test`.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { parseDiff, diffTotals } from '../src/lib/diff.ts'

const MODIFIED = `diff --git a/main.go b/main.go
index 1111111..2222222 100644
--- a/main.go
+++ b/main.go
@@ -1,4 +1,5 @@ package main
 package main
-import "fmt"
+import (
+	"fmt"
+)

 func main() {}
`

test('modified file: counts, line numbers, section', () => {
  const [f, ...rest] = parseDiff(MODIFIED)
  assert.equal(rest.length, 0)
  assert.equal(f.path, 'main.go')
  assert.equal(f.status, 'modified')
  assert.equal(f.additions, 3)
  assert.equal(f.deletions, 1)
  assert.equal(f.hunks.length, 1)
  const h = f.hunks[0]
  assert.equal(h.section, 'package main')
  assert.deepEqual(h.lines.map((l) => l.kind), ['ctx', 'del', 'add', 'add', 'add', 'ctx', 'ctx'])
  assert.deepEqual(h.lines[0], { kind: 'ctx', text: 'package main', oldNo: 1, newNo: 1 })
  assert.equal(h.lines[1].oldNo, 2)
  assert.equal(h.lines[2].newNo, 2)
  assert.equal(h.lines[4].newNo, 4)
  // the empty context line (a single space in the diff)
  assert.deepEqual(h.lines[5], { kind: 'ctx', text: '', oldNo: 3, newNo: 5 })
})

test('new, deleted, renamed and binary files', () => {
  const text = `diff --git a/hello.txt b/hello.txt
new file mode 100644
index 0000000..3b18e51
--- /dev/null
+++ b/hello.txt
@@ -0,0 +1 @@
+hello from akili
diff --git a/old.txt b/old.txt
deleted file mode 100644
index 3b18e51..0000000
--- a/old.txt
+++ /dev/null
@@ -1,2 +0,0 @@
-one
-two
diff --git a/src/a.ts b/src/b.ts
similarity index 90%
rename from src/a.ts
rename to src/b.ts
index 1..2 100644
--- a/src/a.ts
+++ b/src/b.ts
@@ -3 +3 @@
-x
+y
diff --git a/logo.png b/logo.png
new file mode 100644
index 0000000..abc
Binary files /dev/null and b/logo.png differ
diff --git a/moved.bin b/renamed.bin
similarity index 100%
rename from moved.bin
rename to renamed.bin
`
  const files = parseDiff(text)
  assert.equal(files.length, 5)
  const [added, deleted, renamed, binary, pureRename] = files
  assert.equal(added.status, 'added')
  assert.equal(added.path, 'hello.txt')
  assert.equal(added.additions, 1)
  assert.equal(added.hunks[0].lines[0].newNo, 1)
  assert.equal(deleted.status, 'deleted')
  assert.equal(deleted.path, 'old.txt')
  assert.equal(deleted.deletions, 2)
  assert.equal(renamed.status, 'renamed')
  assert.equal(renamed.oldPath, 'src/a.ts')
  assert.equal(renamed.path, 'src/b.ts')
  assert.deepEqual([renamed.additions, renamed.deletions], [1, 1])
  assert.ok(renamed.notes.includes('similarity index 90%'))
  assert.equal(binary.binary, true)
  assert.equal(binary.status, 'added')
  assert.equal(binary.path, 'logo.png')
  assert.equal(binary.hunks.length, 0)
  assert.equal(pureRename.status, 'renamed')
  assert.equal(pureRename.path, 'renamed.bin')
  assert.deepEqual(diffTotals(files), { files: 5, additions: 2, deletions: 3 })
})

test('lines that look like headers inside a hunk are content', () => {
  const text = `diff --git a/notes.md b/notes.md
--- a/notes.md
+++ b/notes.md
@@ -1,2 +1,2 @@
--- old heading
++++ new heading
 unchanged
`
  const [f] = parseDiff(text)
  assert.equal(f.deletions, 1)
  assert.equal(f.additions, 1)
  assert.equal(f.hunks[0].lines[0].text, '-- old heading')
  assert.equal(f.hunks[0].lines[1].text, '+++ new heading')
})

test('no newline marker, multiple hunks, CRLF', () => {
  const text = [
    'diff --git a/a b/a',
    '--- a/a',
    '+++ b/a',
    '@@ -1 +1 @@',
    '-a',
    '\\ No newline at end of file',
    '+b',
    '\\ No newline at end of file',
    '@@ -10,2 +10,3 @@ func x()',
    ' k',
    '+l',
    ' m',
    '',
  ].join('\r\n')
  const [f] = parseDiff(text)
  assert.equal(f.hunks.length, 2)
  assert.deepEqual(f.hunks[0].lines.map((l) => l.kind), ['del', 'meta', 'add', 'meta'])
  assert.equal(f.hunks[1].section, 'func x()')
  assert.equal(f.hunks[1].lines[1].newNo, 11)
  assert.equal(f.hunks[1].lines[2].oldNo, 11)
})

test('paths with spaces and quoted paths', () => {
  const files = parseDiff(`diff --git a/my file.txt b/my file.txt
--- a/my file.txt
+++ b/my file.txt
@@ -1 +1 @@
-a
+b
diff --git "a/tab\\tname" "b/tab\\tname"
--- "a/tab\\tname"
+++ "b/tab\\tname"
@@ -1 +1 @@
-a
+b
`)
  assert.equal(files[0].path, 'my file.txt')
  assert.equal(files[1].path, 'tab\tname')
})

test('plain unified diff without git headers, and garbage', () => {
  const files = parseDiff(`--- a.txt\t2024-01-01 00:00:00
+++ a.txt\t2024-01-02 00:00:00
@@ -1 +1 @@
-x
+y
--- b.txt
+++ b.txt
@@ -1 +1,2 @@
 y
+z
`)
  assert.equal(files.length, 2)
  assert.equal(files[0].path, 'a.txt')
  assert.equal(files[1].path, 'b.txt')
  assert.equal(files[1].additions, 1)
  assert.deepEqual(parseDiff(''), [])
  assert.deepEqual(parseDiff('nothing to see here\nat all'), [])
  // a truncated hunk still yields what was there
  const [t] = parseDiff('diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1,10 +1,10 @@\n-one\n+uno\n')
  assert.deepEqual([t.additions, t.deletions], [1, 1])
})
