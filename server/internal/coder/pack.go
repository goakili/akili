// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package coder

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/goakili/akili/server/internal/gitid"
)

// Pushed commits must carry the agent's git identity, which the sandbox could otherwise override
// (git commit --author, git -c user.email). The pack streams to the forge while a copy is parsed;
// its trailer is held back until every commit passed, and a pack without one is rejected whole.

const (
	objCommit   = 1
	objTree     = 2
	objBlob     = 3
	objTag      = 4
	objOfsDelta = 6
	objRefDelta = 7

	packTrailerLen = sha1.Size
	maxCommitSize  = 1 << 20
	maxCommitBytes = 64 << 20 // all commits of one push, kept as delta bases
)

var errThinPack = errors.New("the pack has deltas against objects outside it (thin pack); push again, the proxy asks for a full pack")

type countingReader struct {
	r *bufio.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func (c *countingReader) ReadByte() (byte, error) {
	b, err := c.r.ReadByte()
	if err == nil {
		c.n++
	}
	return b, err
}

func malformed(format string, a ...any) error { return fmt.Errorf("malformed pack: "+format, a...) }

// inspectPack parses a pack and calls check with the id and body of every commit in it, including
// commits stored as deltas. An empty body (a push that only moves a ref) has no commits.
func inspectPack(r io.Reader, check func(id string, commit []byte) error) error {
	cr := &countingReader{r: bufio.NewReaderSize(r, 64<<10)}
	var hdr [12]byte
	if n, err := io.ReadFull(cr, hdr[:]); n == 0 && err == io.EOF {
		return nil
	} else if err != nil {
		return malformed("%v", err)
	}
	if string(hdr[:4]) != "PACK" {
		return malformed("bad signature")
	}
	if v := binary.BigEndian.Uint32(hdr[4:8]); v != 2 && v != 3 {
		return malformed("unsupported version %d", v)
	}
	count := binary.BigEndian.Uint32(hdr[8:12])
	types := map[int64]int{}      // resolved object type by pack offset
	commits := map[int64][]byte{} // commit bodies by offset, for later deltas
	byID := map[string]int64{}    // offset by object id, for REF_DELTA bases
	var kept int
	var zr io.ReadCloser
	for range count {
		off := cr.n
		typ, size, err := readObjectHeader(cr)
		if err != nil {
			return err
		}
		base := int64(-1)
		switch typ {
		case objCommit, objTree, objBlob, objTag:
		case objOfsDelta:
			rel, err := readOfsDelta(cr)
			if err != nil {
				return err
			}
			base = off - rel
			if _, ok := types[base]; rel <= 0 || !ok {
				return malformed("delta base not in the pack")
			}
		case objRefDelta:
			var id [20]byte
			if _, err := io.ReadFull(cr, id[:]); err != nil {
				return malformed("%v", err)
			}
			o, ok := byID[hex.EncodeToString(id[:])]
			if !ok {
				return errThinPack
			}
			base = o
		default:
			return malformed("unknown object type %d", typ)
		}
		if zr == nil {
			zr, err = zlib.NewReader(cr)
		} else {
			err = zr.(zlib.Resetter).Reset(cr, nil)
		}
		if err != nil {
			return malformed("%v", err)
		}
		resolved := typ
		if base >= 0 {
			resolved = types[base]
		}
		types[off] = resolved
		switch {
		case resolved == objCommit:
			if size > maxCommitSize {
				return malformed("commit too large")
			}
			data, err := io.ReadAll(io.LimitReader(zr, size+1))
			if err != nil || int64(len(data)) != size {
				return malformed("object size mismatch")
			}
			if base >= 0 {
				if data, err = applyDelta(commits[base], data); err != nil {
					return err
				}
			}
			if kept += len(data); kept > maxCommitBytes {
				return errors.New("push has too many commits to inspect; push in smaller steps")
			}
			commits[off] = data
			id := objectID("commit", data)
			byID[id] = off
			if err := check(id, data); err != nil {
				return err
			}
		case base < 0:
			// Hashed so a later REF_DELTA can name it; delta results are never bases in a full pack.
			h := sha1.New()
			_, _ = io.WriteString(h, typeName(typ)+" "+strconv.FormatInt(size, 10)+"\x00")
			if n, err := io.Copy(h, io.LimitReader(zr, size+1)); err != nil || n != size {
				return malformed("object size mismatch")
			}
			byID[hex.EncodeToString(h.Sum(nil))] = off
		default:
			if n, err := io.Copy(io.Discard, io.LimitReader(zr, size+1)); err != nil || n != size {
				return malformed("object size mismatch")
			}
		}
	}
	var trailer [packTrailerLen]byte
	if _, err := io.ReadFull(cr, trailer[:]); err != nil {
		return malformed("missing trailer")
	}
	return nil
}

func readObjectHeader(r io.ByteReader) (int, int64, error) {
	c, err := r.ReadByte()
	if err != nil {
		return 0, 0, malformed("%v", err)
	}
	typ := int(c>>4) & 7
	size := int64(c & 0x0f)
	for shift := 4; c&0x80 != 0; shift += 7 {
		if shift > 53 {
			return 0, 0, malformed("object size overflow")
		}
		if c, err = r.ReadByte(); err != nil {
			return 0, 0, malformed("%v", err)
		}
		size |= int64(c&0x7f) << shift
	}
	return typ, size, nil
}

func readOfsDelta(r io.ByteReader) (int64, error) {
	c, err := r.ReadByte()
	if err != nil {
		return 0, malformed("%v", err)
	}
	off := int64(c & 0x7f)
	for c&0x80 != 0 {
		if off > 1<<48 {
			return 0, malformed("delta offset overflow")
		}
		if c, err = r.ReadByte(); err != nil {
			return 0, malformed("%v", err)
		}
		off = (off+1)<<7 | int64(c&0x7f)
	}
	return off, nil
}

// applyDelta rebuilds an object from its base and a git delta (copy and insert instructions).
func applyDelta(base, delta []byte) ([]byte, error) {
	src, delta, ok := deltaSize(delta)
	if !ok || src != uint64(len(base)) {
		return nil, malformed("delta base size mismatch")
	}
	dst, delta, ok := deltaSize(delta)
	if !ok || dst > maxCommitSize {
		return nil, malformed("bad delta result size")
	}
	out := make([]byte, 0, dst)
	for len(delta) > 0 {
		op := delta[0]
		delta = delta[1:]
		switch {
		case op&0x80 != 0:
			var off, n uint64
			for i := range 7 {
				if op&(1<<i) == 0 {
					continue
				}
				if len(delta) == 0 {
					return nil, malformed("truncated delta")
				}
				if i < 4 {
					off |= uint64(delta[0]) << (8 * i)
				} else {
					n |= uint64(delta[0]) << (8 * (i - 4))
				}
				delta = delta[1:]
			}
			if n == 0 {
				n = 0x10000
			}
			if off+n > uint64(len(base)) {
				return nil, malformed("delta copy out of range")
			}
			out = append(out, base[off:off+n]...)
		case op != 0:
			if int(op) > len(delta) {
				return nil, malformed("truncated delta")
			}
			out = append(out, delta[:op]...)
			delta = delta[op:]
		default:
			return nil, malformed("reserved delta instruction")
		}
		if uint64(len(out)) > dst {
			return nil, malformed("delta result too large")
		}
	}
	if uint64(len(out)) != dst {
		return nil, malformed("delta result size mismatch")
	}
	return out, nil
}

func deltaSize(b []byte) (uint64, []byte, bool) {
	var v uint64
	for i, c := range b {
		if i > 9 {
			break
		}
		v |= uint64(c&0x7f) << (7 * i)
		if c&0x80 == 0 {
			return v, b[i+1:], true
		}
	}
	return 0, nil, false
}

func typeName(t int) string {
	switch t {
	case objCommit:
		return "commit"
	case objTree:
		return "tree"
	case objBlob:
		return "blob"
	default:
		return "tag"
	}
}

func objectID(typ string, data []byte) string {
	h := sha1.New()
	_, _ = io.WriteString(h, typ+" "+strconv.Itoa(len(data))+"\x00")
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// identityCheck refuses commits whose author or committer email is not want's. Exactly one < and >
// are required so git and the forges cannot read a different address than this check did.
func identityCheck(want gitid.Identity) func(id string, commit []byte) error {
	return func(id string, commit []byte) error {
		hdr, _, _ := bytes.Cut(commit, []byte("\n\n"))
		roles := 0
		for _, line := range strings.Split(string(hdr), "\n") {
			role, rest, _ := strings.Cut(line, " ")
			if role != "author" && role != "committer" {
				continue
			}
			roles++
			email := ""
			if strings.Count(rest, "<") == 1 && strings.Count(rest, ">") == 1 {
				_, after, _ := strings.Cut(rest, "<")
				email, _, _ = strings.Cut(after, ">")
			}
			if !strings.EqualFold(email, want.Email) {
				if len(email) > 100 {
					email = email[:100] + "..."
				}
				return fmt.Errorf("commit %s has %s %q, but this agent commits as %s; commit with git_commit and do not override the git identity",
					id[:12], role, email, want)
			}
		}
		if roles < 2 {
			return fmt.Errorf("commit %s has no author or committer", id[:12])
		}
		return nil
	}
}

// verifiedPack passes a pack through while inspectPack reads a copy, holding back the trailer until
// the inspection succeeded. Read fails with the inspection error otherwise.
type verifiedPack struct {
	src  io.Reader
	pw   *io.PipeWriter
	done chan error
	res  error
	buf  []byte

	mu      sync.Mutex
	refused error

	held []byte
	out  []byte
	eof  bool
	err  error
}

func newVerifiedPack(src io.Reader, check func(id string, commit []byte) error) *verifiedPack {
	pr, pw := io.Pipe()
	v := &verifiedPack{src: src, pw: pw, done: make(chan error, 1), buf: make([]byte, 32<<10)}
	go func() {
		err := inspectPack(pr, check)
		if err == nil {
			_, _ = io.Copy(io.Discard, pr) // so the writer never blocks on bytes after the trailer
		}
		if err != nil {
			pr.CloseWithError(err)
		} else {
			pr.Close()
		}
		v.done <- err
	}()
	return v
}

func (v *verifiedPack) result() error {
	if v.done != nil {
		v.res = <-v.done
		v.done = nil
		v.mu.Lock()
		v.refused = v.res
		v.mu.Unlock()
	}
	return v.res
}

// Refused returns the inspection error, if the pack was refused. The transport may still be
// reading the body when the request returns, hence the lock.
func (v *verifiedPack) Refused() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.refused
}

func (v *verifiedPack) Read(p []byte) (int, error) {
	for {
		if len(v.out) > 0 {
			n := copy(p, v.out)
			v.out = v.out[n:]
			return n, nil
		}
		if v.err != nil {
			return 0, v.err
		}
		if v.eof {
			return 0, io.EOF
		}
		n, err := v.src.Read(v.buf)
		if n > 0 {
			if _, werr := v.pw.Write(v.buf[:n]); werr != nil {
				v.err = v.result()
				if v.err == nil {
					v.err = werr
				}
				continue
			}
			v.held = append(v.held, v.buf[:n]...)
			if k := len(v.held) - packTrailerLen; k > 0 {
				v.out = append(v.out[:0], v.held[:k]...)
				v.held = append(v.held[:0], v.held[k:]...)
			}
		}
		switch {
		case err == io.EOF:
			_ = v.pw.Close()
			// The last read may also have queued output (data and EOF together): the trailer follows it.
			if v.err = v.result(); v.err == nil {
				v.out, v.held, v.eof = append(v.out, v.held...), nil, true
			}
		case err != nil:
			v.pw.CloseWithError(err)
			v.err = err
		}
	}
}

// Close stops the inspection when the upstream request ends early.
func (v *verifiedPack) Close() error {
	v.pw.CloseWithError(errors.New("push aborted"))
	return nil
}

// advertiseNoThin copies a receive-pack ref advertisement and adds the no-thin capability, so git
// sends packs whose deltas resolve inside the pack and every commit can be inspected.
func advertiseNoThin(w io.Writer, r io.Reader) error {
	br := bufio.NewReader(r)
	for {
		var hdr [4]byte
		if k, err := io.ReadFull(br, hdr[:]); err != nil {
			_, _ = w.Write(hdr[:k])
			return nil
		}
		n, err := strconv.ParseUint(string(hdr[:]), 16, 16)
		if err != nil || (n > 0 && n < 4) {
			_, _ = w.Write(hdr[:]) // not an advertisement git understands: pass it through as is
			break
		}
		if n == 0 {
			if _, err := w.Write(hdr[:]); err != nil {
				return err
			}
			continue
		}
		line := make([]byte, n-4)
		if k, err := io.ReadFull(br, line); err != nil {
			_, _ = w.Write(hdr[:])
			_, _ = w.Write(line[:k])
			return nil
		}
		if bytes.IndexByte(line, 0) < 0 {
			if _, err := fmt.Fprintf(w, "%s%s", hdr[:], line); err != nil {
				return err
			}
			continue
		}
		s := strings.TrimSuffix(string(line), "\n") + " no-thin\n"
		if _, err := fmt.Fprintf(w, "%04x%s", len(s)+4, s); err != nil {
			return err
		}
		break
	}
	_, err := io.Copy(w, br)
	return err
}
