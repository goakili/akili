// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package mcp connects the control plane to MCP (Model Context Protocol) servers and exposes their
// tools to agents as remote tools named "mcp__<server>__<tool>". Calls run here, never on agents,
// so the servers' credentials stay on the control plane.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ProtocolVersion is the MCP revision Akili speaks.
const ProtocolVersion = "2025-06-18"

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int64 `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	ID     *int64          `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// transport sends one request and returns its response (or sends a notification when id is nil).
type transport interface {
	roundTrip(ctx context.Context, req rpcRequest) (*rpcResponse, error)
	close() error
}

// Client is a connection to one MCP server. Calls are serialised.
type Client struct {
	mu     sync.Mutex
	t      transport
	nextID int64
}

// Tool is a tool a server offers.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Annotations *struct {
		ReadOnlyHint    bool `json:"readOnlyHint"`
		DestructiveHint bool `json:"destructiveHint"`
	} `json:"annotations"`
}

func (c *Client) call(ctx context.Context, method string, params, out any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextID++
	id := c.nextID
	resp, err := c.t.roundTrip(ctx, rpcRequest{JSONRPC: "2.0", ID: &id, Method: method, Params: params})
	if err != nil {
		return err
	}
	if resp.Error != nil {
		return fmt.Errorf("mcp %s: %s (%d)", method, resp.Error.Message, resp.Error.Code)
	}
	if out != nil {
		return json.Unmarshal(resp.Result, out)
	}
	return nil
}

func (c *Client) notify(ctx context.Context, method string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.t.roundTrip(ctx, rpcRequest{JSONRPC: "2.0", Method: method})
	return err
}

// initialize performs the MCP handshake.
func (c *Client) initialize(ctx context.Context) error {
	var res struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	err := c.call(ctx, "initialize", map[string]any{"protocolVersion": ProtocolVersion, "capabilities": map[string]any{},
		"clientInfo": map[string]string{"name": "akili", "version": "2"}}, &res)
	if err != nil {
		return err
	}
	return c.notify(ctx, "notifications/initialized")
}

// ListTools returns every tool (following pagination).
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	var all []Tool
	cursor := ""
	for range 50 {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var res struct {
			Tools      []Tool `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		if err := c.call(ctx, "tools/list", params, &res); err != nil {
			return nil, err
		}
		all = append(all, res.Tools...)
		if res.NextCursor == "" {
			return all, nil
		}
		cursor = res.NextCursor
	}
	return all, nil
}

// CallTool runs a tool and returns its text output.
func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage) (string, bool, error) {
	var res struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	if err := c.call(ctx, "tools/call", map[string]any{"name": name, "arguments": args}, &res); err != nil {
		return "", true, err
	}
	var b strings.Builder
	for _, part := range res.Content {
		switch part.Type {
		case "text":
			b.WriteString(part.Text)
			b.WriteString("\n")
		default:
			fmt.Fprintf(&b, "[%s content omitted]\n", part.Type)
		}
	}
	return strings.TrimRight(b.String(), "\n"), res.IsError, nil
}

// Close ends the connection (and the process, for stdio).
func (c *Client) Close() error { return c.t.close() }

// StartStdio launches a server process. env is its whole environment: nothing is inherited from the
// control plane, whose own secrets must never reach a tool server.
func StartStdio(ctx context.Context, path string, args, env []string, dir string) (*Client, error) {
	cmd := exec.Command(path, args...)
	cmd.Env, cmd.Dir = env, dir
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &limitedWriter{w: &stderr, n: 16 << 10}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	t := &stdio{cmd: cmd, stdin: stdin, stderr: &stderr, lines: make(chan []byte, 64), done: make(chan struct{})}
	go t.read(stdout)
	c := &Client{t: t}
	ictx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := c.initialize(ictx); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("%w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}
	return c, nil
}

type limitedWriter struct {
	w io.Writer
	n int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.n > 0 {
		k := min(len(p), l.n)
		_, _ = l.w.Write(p[:k])
		l.n -= k
	}
	return len(p), nil
}

type stdio struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stderr *bytes.Buffer
	lines  chan []byte
	done   chan struct{}
	once   sync.Once
}

func (s *stdio) read(r io.Reader) {
	defer close(s.done)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 8<<20)
	for sc.Scan() {
		line := append([]byte(nil), sc.Bytes()...)
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		select {
		case s.lines <- line:
		case <-time.After(time.Minute):
			return // nobody is reading: the server is misbehaving
		}
	}
}

func (s *stdio) roundTrip(ctx context.Context, req rpcRequest) (*rpcResponse, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if _, err := s.stdin.Write(append(b, '\n')); err != nil {
		return nil, fmt.Errorf("mcp server stopped: %w", err)
	}
	if req.ID == nil {
		return &rpcResponse{}, nil
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.done:
			return nil, errors.New("mcp server exited: " + strings.TrimSpace(s.stderr.String()))
		case line := <-s.lines:
			var resp rpcResponse
			if json.Unmarshal(line, &resp) != nil || resp.ID == nil || *resp.ID != *req.ID {
				continue // a log line, a notification or a server request: not ours
			}
			return &resp, nil
		}
	}
}

func (s *stdio) close() error {
	s.once.Do(func() {
		_ = s.stdin.Close()
		if s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
		_ = s.cmd.Wait()
	})
	return nil
}

// DialHTTP connects to a Streamable HTTP server. headers are sent with every request (e.g.
// Authorization).
func DialHTTP(ctx context.Context, url string, headers map[string]string) (*Client, error) {
	t := &httpT{url: url, headers: headers, client: &http.Client{Timeout: 5 * time.Minute}}
	c := &Client{t: t}
	ictx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := c.initialize(ictx); err != nil {
		return nil, err
	}
	return c, nil
}

type httpT struct {
	url     string
	headers map[string]string
	client  *http.Client
	session string
}

func (h *httpT) roundTrip(ctx context.Context, req rpcRequest) (*rpcResponse, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, h.url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	hr.Header.Set("Content-Type", "application/json")
	hr.Header.Set("Accept", "application/json, text/event-stream")
	hr.Header.Set("MCP-Protocol-Version", ProtocolVersion)
	if h.session != "" {
		hr.Header.Set("Mcp-Session-Id", h.session)
	}
	for k, v := range h.headers {
		hr.Header.Set(k, v)
	}
	resp, err := h.client.Do(hr)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		h.session = sid
	}
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("mcp http %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	if req.ID == nil {
		return &rpcResponse{}, nil
	}
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 64<<10), 8<<20)
		for sc.Scan() {
			data, ok := strings.CutPrefix(sc.Text(), "data:")
			if !ok {
				continue
			}
			var r rpcResponse
			if json.Unmarshal([]byte(strings.TrimSpace(data)), &r) == nil && r.ID != nil && *r.ID == *req.ID {
				return &r, nil
			}
		}
		return nil, errors.New("mcp http: stream ended without a response")
	}
	var r rpcResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (h *httpT) close() error { return nil }
