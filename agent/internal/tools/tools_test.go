// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goakili/akili/proto"
)

func executor(t *testing.T) (*Executor, string) {
	t.Helper()
	wd := t.TempDir()
	wd, _ = filepath.EvalSymlinks(wd) // macOS: /var -> /private/var
	pol := proto.Policy{Name: "test", Tools: proto.Rule{Allow: []string{"*"}}, MaxRisk: proto.RiskHigh,
		Paths:    proto.Rule{Allow: []string{"$WORKDIR/**"}, Deny: []string{"$WORKDIR/secret/**"}},
		Commands: proto.Rule{Allow: []string{"*"}}, AllowShellMeta: true, Domains: proto.Rule{Allow: []string{"*"}}}
	return &Executor{Workdir: wd, Policy: pol, Facts: func() proto.HostFacts { return proto.HostFacts{} }}, wd
}

func run(t *testing.T, e *Executor, tool string, in any) Result {
	t.Helper()
	b, _ := json.Marshal(in)
	return e.Run(context.Background(), tool, b)
}

func TestSymlinkEscapeIsDenied(t *testing.T) {
	e, wd := executor(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "shadow"), []byte("root:x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "shadow"), filepath.Join(wd, "innocent.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(wd, "dir")); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		tool string
		in   any
	}{
		{proto.ToolFSRead, proto.FSReadInput{Path: "innocent.txt"}},
		{proto.ToolFSWrite, proto.FSWriteInput{Path: "dir/new.txt", Content: "x"}},
		{proto.ToolFSList, proto.FSListInput{Path: "dir"}},
		{proto.ToolShell, proto.ShellInput{Command: "pwd", Cwd: "dir"}},
	} {
		r := run(t, e, c.tool, c.in)
		if !r.IsError || !strings.Contains(r.Output, "not allowed") {
			t.Errorf("%s through a symlink out of the workdir: got %q", c.tool, r.Output)
		}
	}
	if _, err := os.Stat(filepath.Join(outside, "new.txt")); err == nil {
		t.Fatal("write escaped the workdir through a symlinked directory")
	}
}

func TestDeniedSubdirectory(t *testing.T) {
	e, wd := executor(t)
	_ = os.MkdirAll(filepath.Join(wd, "secret"), 0o755)
	_ = os.WriteFile(filepath.Join(wd, "secret", "key"), []byte("hunter2"), 0o600)
	if r := run(t, e, proto.ToolFSRead, proto.FSReadInput{Path: "secret/key"}); !r.IsError {
		t.Fatalf("read of a denied path succeeded: %q", r.Output)
	}
	if r := run(t, e, proto.ToolSearch, proto.SearchInput{Pattern: "hunter2"}); strings.Contains(r.Output, "hunter2") {
		t.Fatalf("search leaked a denied file: %q", r.Output)
	}
}

func TestWriteEditRead(t *testing.T) {
	e, _ := executor(t)
	if r := run(t, e, proto.ToolFSWrite, proto.FSWriteInput{Path: "a/b.txt", Content: "hello world\nhello again\n"}); r.IsError {
		t.Fatal(r.Output)
	}
	if r := run(t, e, proto.ToolFSEdit, proto.FSEditInput{Path: "a/b.txt", OldString: "hello", NewString: "bye"}); !r.IsError {
		t.Fatal("ambiguous edit should fail")
	}
	if r := run(t, e, proto.ToolFSEdit, proto.FSEditInput{Path: "a/b.txt", OldString: "hello world", NewString: "bye world"}); r.IsError {
		t.Fatal(r.Output)
	}
	r := run(t, e, proto.ToolFSRead, proto.FSReadInput{Path: "a/b.txt"})
	if r.IsError || !strings.Contains(r.Output, "bye world") || !strings.Contains(r.Output, "hello again") {
		t.Fatalf("unexpected content: %q", r.Output)
	}
	if r := run(t, e, proto.ToolSearch, proto.SearchInput{Pattern: "again", Glob: "*.txt"}); !strings.Contains(r.Output, "b.txt:2:") {
		t.Fatalf("search: %q", r.Output)
	}
}

func TestShellEnvironmentIsScrubbed(t *testing.T) {
	e, _ := executor(t)
	t.Setenv("AKILI_SECRET_CANARY", "leaked-canary")
	r := run(t, e, proto.ToolShell, proto.ShellInput{Command: "env"})
	if r.IsError || strings.Contains(r.Output, "leaked-canary") {
		t.Fatalf("agent environment leaked into a shell tool: %q", r.Output)
	}
}

func TestShellTimeoutKillsProcessGroup(t *testing.T) {
	e, _ := executor(t)
	r := run(t, e, proto.ToolShell, proto.ShellInput{Command: "sleep 30 & sleep 30", TimeoutSec: 1})
	if !r.IsError || !strings.Contains(r.Output, "timed out") {
		t.Fatalf("got %q", r.Output)
	}
}

func TestHTTPFetchRefusesPrivateAddresses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("internal")) }))
	defer srv.Close()
	e, _ := executor(t)
	r := run(t, e, proto.ToolHTTPFetch, proto.HTTPFetchInput{URL: srv.URL})
	if !r.IsError || strings.Contains(r.Output, "internal") {
		t.Fatalf("fetched a loopback address: %q", r.Output)
	}
	r = run(t, e, proto.ToolHTTPFetch, proto.HTTPFetchInput{URL: "http://169.254.169.254/latest/meta-data/"})
	if !r.IsError {
		t.Fatalf("fetched the cloud metadata address: %q", r.Output)
	}
}

func TestOutputIsTruncated(t *testing.T) {
	r := truncate(strings.Repeat("x", MaxOutput*3), false)
	if !r.Truncated || len(r.Output) > MaxOutput+200 {
		t.Fatalf("output not truncated: %d bytes", len(r.Output))
	}
}

// The default workdir lives inside the state directory; a permissive policy must still never expose
// state.json (the agent's private key), directly, by traversal or through a symlink.
func TestStateDirIsProtectedWhateverThePolicy(t *testing.T) {
	state := t.TempDir()
	state, _ = filepath.EvalSymlinks(state)
	wd := filepath.Join(state, "work")
	if err := os.MkdirAll(wd, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "state.json"), []byte(`{"private_key":"TEST-KEY-MATERIAL"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(state, "state.json"), filepath.Join(wd, "link.json")); err != nil {
		t.Fatal(err)
	}
	pol := proto.Policy{Name: "wide-open", Tools: proto.Rule{Allow: []string{"*"}}, MaxRisk: proto.RiskHigh,
		Paths: proto.Rule{Allow: []string{"/**"}}, Commands: proto.Rule{Allow: []string{"*"}}}
	e := &Executor{Workdir: wd, Policy: pol, Protected: []string{state}, Facts: func() proto.HostFacts { return proto.HostFacts{} }}

	for _, c := range []struct {
		tool string
		in   any
	}{
		{proto.ToolFSRead, proto.FSReadInput{Path: filepath.Join(state, "state.json")}},
		{proto.ToolFSRead, proto.FSReadInput{Path: "../state.json"}},
		{proto.ToolFSRead, proto.FSReadInput{Path: "link.json"}},
		{proto.ToolFSList, proto.FSListInput{Path: state}},
		{proto.ToolFSWrite, proto.FSWriteInput{Path: "../state.json", Content: "{}"}},
		{proto.ToolFSWrite, proto.FSWriteInput{Path: "../.ssh/authorized_keys", Content: "ssh-ed25519 AAAA"}},
	} {
		r := run(t, e, c.tool, c.in)
		if !r.IsError || strings.Contains(r.Output, "TEST-KEY-MATERIAL") {
			t.Errorf("%s %+v was not refused: %q", c.tool, c.in, r.Output)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(state, "state.json")); !strings.Contains(string(b), "TEST-KEY-MATERIAL") {
		t.Fatal("state.json was modified")
	}
	if r := run(t, e, proto.ToolFSWrite, proto.FSWriteInput{Path: "ok.txt", Content: "fine"}); r.IsError {
		t.Fatalf("the workdir inside the state dir is not usable: %s", r.Output)
	}
}

func TestShellOutputIsRedacted(t *testing.T) {
	e, _ := executor(t)
	e.ShellEnv = []string{"DEPLOY_TOKEN=akili-test-secret-DO-NOT-USE"}
	r := run(t, e, proto.ToolShell, proto.ShellInput{Command: "echo token=$DEPLOY_TOKEN; echo ghp_0123456789abcdefghijklmnopqrstuvwxyzAB"})
	if strings.Contains(r.Output, "DO-NOT-USE") || strings.Contains(r.Output, "ghp_0123") || !strings.Contains(r.Output, proto.Redacted) {
		t.Fatalf("secret reached the output: %q", r.Output)
	}
}
