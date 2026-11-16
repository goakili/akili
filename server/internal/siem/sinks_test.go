// Copyright (c) 2026 Jonas Kaninda.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package siem

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/goakili/akili/server/internal/models"
)

func events() []Event {
	return []Event{
		{Source: "akili", AuditLog: models.AuditLog{ID: 1, OrganizationID: "org_1", Action: "auth.login", CreatedAt: time.Now(), Hash: "h1"}},
		{Source: "akili", AuditLog: models.AuditLog{ID: 2, OrganizationID: "org_1", Action: "tool.denied", CreatedAt: time.Now(), PrevHash: "h1", Hash: "h2"}},
	}
}

func TestWebhookSinkSignsBatch(t *testing.T) {
	var got []Event
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		m := hmac.New(sha256.New, []byte("s3"))
		m.Write(body)
		if r.Header.Get("X-Akili-Signature") != "sha256="+hex.EncodeToString(m.Sum(nil)) || r.Header.Get("Authorization") != "Splunk tok" {
			w.WriteHeader(401)
			return
		}
		var env struct{ Events []Event }
		_ = json.Unmarshal(body, &env)
		got = env.Events
	}))
	defer srv.Close()
	s := &WebhookSink{URL: srv.URL, Secret: "s3", Authorization: "Splunk tok"}
	if err := s.Send(context.Background(), events()); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].PrevHash != "h1" || got[1].Source != "akili" {
		t.Fatalf("received %+v", got)
	}
	if err := (&WebhookSink{URL: srv.URL, Secret: "wrong"}).Send(context.Background(), events()); err == nil {
		t.Fatal("a rejected delivery reported success")
	}
}

func TestSyslogSinkOctetCounting(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	lines := make(chan string, 2)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		r := bufio.NewReader(c)
		for range 2 {
			n, _ := r.ReadString(' ')
			size, _ := strconv.Atoi(strings.TrimSpace(n))
			buf := make([]byte, size)
			_, _ = io.ReadFull(r, buf)
			lines <- string(buf)
		}
	}()
	s, err := NewSyslogSink("tcp://" + ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Send(context.Background(), events()); err != nil {
		t.Fatal(err)
	}
	first, second := <-lines, <-lines
	if !strings.HasPrefix(first, "<86>1 ") || !strings.Contains(first, " akili - auth.login - {") {
		t.Fatalf("first message: %q", first)
	}
	if !strings.HasPrefix(second, "<85>1 ") { // denials are notice
		t.Fatalf("second message: %q", second)
	}
	if _, err := NewSyslogSink("http://x:1"); err == nil {
		t.Fatal("bad scheme accepted")
	}
}

func TestFileSinkAppendsJSONLines(t *testing.T) {
	p := filepath.Join(t.TempDir(), "audit.jsonl")
	s := &FileSink{Path: p}
	for range 2 {
		if err := s.Send(context.Background(), events()); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile(p)
	if n := strings.Count(string(b), "\n"); n != 4 {
		t.Fatalf("lines = %d", n)
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", st.Mode())
	}
}
