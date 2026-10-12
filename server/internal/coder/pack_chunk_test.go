// Copyright (c) 2026 Jonas Kaninda.
//
// This file is part of Akili.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package coder

import (
	"bytes"
	"io"
	"math/rand"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/goakili/akili/server/internal/gitid"
)

type randChunks struct {
	r   io.Reader
	rnd *rand.Rand
}

func (c *randChunks) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1+c.rnd.Intn(len(p))]
	}
	return c.r.Read(p)
}

func TestVerifiedPackIsLosslessUnderChunking(t *testing.T) {
	agent := identityEnv(agentID)
	dir := repo(t, agent, agent, agent, agent)
	pack := gitRun(t, dir, nil, "HEAD\n", "pack-objects", "--stdout", "--revs", "-q")
	readers := map[string]func() io.Reader{
		"one byte": func() io.Reader { return iotest.OneByteReader(bytes.NewReader(pack)) },
		"half":     func() io.Reader { return iotest.HalfReader(bytes.NewReader(pack)) },
		"data+EOF": func() io.Reader { return iotest.DataErrReader(bytes.NewReader(pack)) },
		"random":   func() io.Reader { return &randChunks{bytes.NewReader(pack), rand.New(rand.NewSource(1))} },
	}
	for name, mk := range readers {
		for _, bufSize := range []int{1, 7, 512, 32 << 10, 1 << 20} {
			v := newVerifiedPack(mk(), identityCheck(agentID))
			var out bytes.Buffer
			buf := make([]byte, bufSize)
			for {
				n, err := v.Read(buf)
				out.Write(buf[:n])
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("%s/%d: %v", name, bufSize, err)
				}
			}
			if !bytes.Equal(out.Bytes(), pack) {
				t.Fatalf("%s/%d: output differs (%d bytes, want %d)", name, bufSize, out.Len(), len(pack))
			}
		}
	}
}

func TestRefusedPackNeverReleasesTrailerUnderChunking(t *testing.T) {
	agent := identityEnv(agentID)
	ceo := identityEnv(gitid.Identity{Name: "CEO", Email: "ceo@example.com"})
	pack := gitRun(t, repo(t, agent, agent, ceo), nil, "HEAD\n", "pack-objects", "--stdout", "--revs", "-q")
	for name, mk := range map[string]func() io.Reader{
		"one byte": func() io.Reader { return iotest.OneByteReader(bytes.NewReader(pack)) },
		"data+EOF": func() io.Reader { return iotest.DataErrReader(bytes.NewReader(pack)) },
		"random":   func() io.Reader { return &randChunks{bytes.NewReader(pack), rand.New(rand.NewSource(2))} },
	} {
		v := newVerifiedPack(mk(), identityCheck(agentID))
		out, err := io.ReadAll(v)
		if err == nil || v.Refused() == nil {
			t.Fatalf("%s: spoofed pack passed", name)
		}
		if len(out) > len(pack)-packTrailerLen {
			t.Fatalf("%s: trailer released (%d of %d bytes)", name, len(out), len(pack))
		}
	}
}

func TestProbe(t *testing.T) {
	if !isProbe(nil, strings.NewReader("")) {
		t.Fatal("a bare flush is git's probe")
	}
	if isProbe([]byte("PACK"), strings.NewReader("")) || isProbe(nil, strings.NewReader("PACK")) {
		t.Fatal("a request with data after the flush is not a probe")
	}
}
