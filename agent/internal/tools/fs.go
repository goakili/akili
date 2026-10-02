// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package tools

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/goakili/akili/proto"
)

const (
	maxReadBytes   = 4 << 20
	maxWriteBytes  = 5 << 20
	defaultLines   = 2000
	maxSearchHits  = 200
	maxSearchFile  = 2 << 20
	maxListEntries = 1000
)

func (e *Executor) fsRead(in proto.FSReadInput) (string, error) {
	p, err := e.resolve(in.Path)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	if st.IsDir() {
		return "", fmt.Errorf("%s is a directory; use fs_list", p)
	}
	if st.Size() > maxReadBytes && in.Limit == 0 {
		return "", fmt.Errorf("file is %d bytes; read it in parts with offset/limit", st.Size())
	}
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	limit := in.Limit
	if limit <= 0 {
		limit = defaultLines
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	var b strings.Builder
	line, shown := 0, 0
	for sc.Scan() {
		if line >= in.Offset && shown < limit {
			if shown == 0 && isBinary(sc.Bytes()) {
				return "", fmt.Errorf("%s looks like a binary file", p)
			}
			fmt.Fprintf(&b, "%6d\t%s\n", line+1, sc.Text())
			shown++
		}
		line++
	}
	if err := sc.Err(); err != nil {
		return b.String(), err
	}
	if line > in.Offset+shown {
		fmt.Fprintf(&b, "… %d more lines (use offset=%d)\n", line-in.Offset-shown, in.Offset+shown)
	}
	return b.String(), nil
}

func (e *Executor) fsList(in proto.FSListInput) (string, error) {
	path := in.Path
	if path == "" {
		path = "."
	}
	p, err := e.resolve(path)
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(p)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", p)
	for i, d := range entries {
		if i >= maxListEntries {
			fmt.Fprintf(&b, "… %d more entries\n", len(entries)-i)
			break
		}
		info, err := d.Info()
		switch {
		case err != nil:
			fmt.Fprintf(&b, "?  %s\n", d.Name())
		case d.IsDir():
			fmt.Fprintf(&b, "d  %s/\n", d.Name())
		case d.Type()&fs.ModeSymlink != 0:
			target, _ := os.Readlink(filepath.Join(p, d.Name()))
			fmt.Fprintf(&b, "l  %s -> %s\n", d.Name(), target)
		default:
			fmt.Fprintf(&b, "f  %s (%d bytes)\n", d.Name(), info.Size())
		}
	}
	return b.String(), nil
}

func (e *Executor) fsWrite(in proto.FSWriteInput) (string, error) {
	if len(in.Content) > maxWriteBytes {
		return "", fmt.Errorf("content exceeds %d bytes", maxWriteBytes)
	}
	p, err := e.resolve(in.Path)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	mode := os.FileMode(0o644)
	if st, err := os.Stat(p); err == nil {
		if st.IsDir() {
			return "", fmt.Errorf("%s is a directory", p)
		}
		mode = st.Mode().Perm()
	}
	if err := atomicWrite(p, []byte(in.Content), mode); err != nil {
		return "", err
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(in.Content), p), nil
}

func (e *Executor) fsEdit(in proto.FSEditInput) (string, error) {
	if in.OldString == "" {
		return "", fmt.Errorf("old_string must not be empty")
	}
	if in.OldString == in.NewString {
		return "", fmt.Errorf("old_string and new_string are identical")
	}
	p, err := e.resolve(in.Path)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	n := bytes.Count(data, []byte(in.OldString))
	switch {
	case n == 0:
		return "", fmt.Errorf("old_string not found in %s", p)
	case n > 1 && !in.ReplaceAll:
		return "", fmt.Errorf("old_string occurs %d times in %s; add context to make it unique or set replace_all", n, p)
	}
	var out []byte
	if in.ReplaceAll {
		out = bytes.ReplaceAll(data, []byte(in.OldString), []byte(in.NewString))
	} else {
		out = bytes.Replace(data, []byte(in.OldString), []byte(in.NewString), 1)
	}
	if err := atomicWrite(p, out, st.Mode().Perm()); err != nil {
		return "", err
	}
	return fmt.Sprintf("replaced %d occurrence(s) in %s", map[bool]int{true: n, false: 1}[in.ReplaceAll], p), nil
}

// atomicWrite writes via a temp file and rename, so a crash never leaves a half-written file.
func atomicWrite(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".akili-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

var skipDirs = map[string]bool{".git": true, "node_modules": true, "vendor": true, ".venv": true, "__pycache__": true}

func (e *Executor) search(ctx context.Context, in proto.SearchInput) (string, error) {
	re, err := regexp.Compile(in.Pattern)
	if err != nil {
		return "", fmt.Errorf("invalid pattern: %w", err)
	}
	root := in.Path
	if root == "" {
		root = "."
	}
	base, err := e.resolve(root)
	if err != nil {
		return "", err
	}
	var hits []string
	err = filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if skipDirs[d.Name()] && p != base {
				return filepath.SkipDir
			}
			if !e.Policy.PathAllowed(e.Workdir, p) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !e.Policy.PathAllowed(e.Workdir, p) {
			return nil
		}
		if in.Glob != "" {
			if ok, _ := filepath.Match(in.Glob, d.Name()); !ok {
				return nil
			}
		}
		if info, err := d.Info(); err != nil || info.Size() > maxSearchFile {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil || isBinary(data) {
			return nil
		}
		for i, line := range strings.Split(string(data), "\n") {
			if re.MatchString(line) {
				if len(line) > 300 {
					line = line[:300] + "…"
				}
				hits = append(hits, fmt.Sprintf("%s:%d:%s", p, i+1, line))
				if len(hits) >= maxSearchHits {
					return fs.SkipAll
				}
			}
		}
		return nil
	})
	if err != nil && err != fs.SkipAll {
		return "", err
	}
	sort.Strings(hits)
	if len(hits) == 0 {
		return "no matches", nil
	}
	out := strings.Join(hits, "\n")
	if len(hits) >= maxSearchHits {
		out += fmt.Sprintf("\n… stopped at %d matches; narrow the pattern or path", maxSearchHits)
	}
	return out, nil
}
