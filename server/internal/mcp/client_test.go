// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The test binary doubles as a fake stdio MCP server when AKILI_FAKE_MCP is set.
func TestMain(m *testing.M) {
	if os.Getenv("AKILI_FAKE_MCP") == "1" {
		fakeServer()
		return
	}
	os.Exit(m.Run())
}

func fakeServer() {
	in := bufio.NewScanner(os.Stdin)
	out := json.NewEncoder(os.Stdout)
	fmt.Fprintln(os.Stderr, "fake mcp starting") // stderr noise must not break the protocol
	for in.Scan() {
		var req struct {
			ID     *int64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(in.Bytes(), &req) != nil || req.ID == nil {
			continue
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": ProtocolVersion, "capabilities": map[string]any{"tools": map[string]any{}},
				"serverInfo": map[string]string{"name": "fake", "version": "1"}}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{
				{"name": "list_things", "description": "List things", "inputSchema": map[string]any{"type": "object"}, "annotations": map[string]any{"readOnlyHint": true}},
				{"name": "delete_thing", "description": "Delete a thing", "inputSchema": map[string]any{"type": "object"}, "annotations": map[string]any{"destructiveHint": true}},
				{"name": "env_probe", "description": "Report the environment", "inputSchema": map[string]any{"type": "object"}, "annotations": map[string]any{"readOnlyHint": true}},
			}}
		case "tools/call":
			var p struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &p)
			text := fmt.Sprintf("called %s with %v", p.Name, p.Arguments)
			if p.Name == "env_probe" {
				text = strings.Join(os.Environ(), "\n")
			}
			result = map[string]any{"content": []map[string]any{{"type": "text", "text": text}}}
		default:
			_ = out.Encode(map[string]any{"jsonrpc": "2.0", "id": *req.ID, "error": map[string]any{"code": -32601, "message": "method not found"}})
			continue
		}
		// A notification first: the client must skip it.
		_ = out.Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/message", "params": map[string]any{"level": "info"}})
		_ = out.Encode(map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": result})
	}
}

func startFake(t *testing.T, env ...string) *Client {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := StartStdio(ctx, exe, nil, append([]string{"AKILI_FAKE_MCP=1"}, env...), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestStdioClient(t *testing.T) {
	t.Setenv("AKILI_CONTROL_PLANE_SECRET", "must-not-leak")
	c := startFake(t, "MIABI_TOKEN=mb_test")
	ctx := context.Background()
	tools, err := c.ListTools(ctx)
	if err != nil || len(tools) != 3 || !tools[0].Annotations.ReadOnlyHint || tools[1].Annotations.ReadOnlyHint {
		t.Fatalf("tools: %+v %v", tools, err)
	}
	out, isErr, err := c.CallTool(ctx, "list_things", json.RawMessage(`{"workspace":"prod"}`))
	if err != nil || isErr || !strings.Contains(out, "called list_things with map[workspace:prod]") {
		t.Fatalf("call: %q %v %v", out, isErr, err)
	}
	env, _, err := c.CallTool(ctx, "env_probe", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(env, "must-not-leak") || !strings.Contains(env, "MIABI_TOKEN=mb_test") {
		t.Fatalf("server environment is not exactly what was given:\n%s", env)
	}
	if err := c.call(ctx, "resources/list", nil, nil); err == nil || !strings.Contains(err.Error(), "method not found") {
		t.Fatalf("rpc error not surfaced: %v", err)
	}
}

func TestCommandAllowlist(t *testing.T) {
	s := &Service{commands: []string{"miabi"}}
	for _, bad := range []string{"bash", "/bin/sh", "miabi; rm -rf /", "../miabi-evil", "python3"} {
		if s.CheckCommand(bad) == nil {
			t.Errorf("%q allowed", bad)
		}
	}
	for _, good := range []string{"miabi", "/usr/local/bin/miabi"} {
		if err := s.CheckCommand(good); err != nil {
			t.Errorf("%q refused: %v", good, err)
		}
	}
}

func TestBinDir(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "miabi")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := &Service{commands: []string{"miabi"}, binDir: dir}
	if _, err := s.resolveCommand("miabi"); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("a binary the server can rewrite was accepted: %v", err)
	}
	if _, err := s.resolveCommand("not-installed-anywhere"); err == nil || !strings.Contains(err.Error(), "AKILI_MCP_BIN_DIR") {
		t.Fatalf("missing command: %v", err)
	}
	if !strings.HasPrefix(s.childPath(), dir+string(os.PathListSeparator)) {
		t.Fatalf("bin dir not first on the child's PATH: %s", s.childPath())
	}
	if os.Geteuid() == 0 {
		t.Skip("root may write anything")
	}
	if err := os.Chmod(bin, 0o555); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if p, err := s.resolveCommand("miabi"); err != nil || p != bin {
		t.Fatalf("read-only binary: %q %v", p, err)
	}
}
