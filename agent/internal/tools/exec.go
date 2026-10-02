// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/goakili/akili/proto"
)

const (
	defaultShellTimeout = 120 * time.Second
	maxShellTimeout     = 600 * time.Second
)

// shell runs a command with a scrubbed environment in its own process group, so a timeout kills the
// whole tree, and the agent's own environment (control-plane URL, anything else) never leaks in.
func (e *Executor) shell(ctx context.Context, in proto.ShellInput) (string, error) {
	cwd := in.Cwd
	if cwd == "" {
		cwd = "."
	}
	dir, err := e.resolve(cwd)
	if err != nil {
		return "", err
	}
	timeout := defaultShellTimeout
	if in.TimeoutSec > 0 {
		timeout = time.Duration(in.TimeoutSec) * time.Second
		if timeout > maxShellTimeout {
			timeout = maxShellTimeout
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.Command("/bin/sh", "-c", in.Command)
	cmd.Dir = dir
	cmd.Env = []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/opt/homebrew/bin:/opt/homebrew/sbin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=" + e.Workdir,
		"LANG=C.UTF-8",
		"TERM=dumb",
		"PAGER=cat",
		"GIT_TERMINAL_PROMPT=0",
	}
	cmd.Env = append(cmd.Env, e.ShellEnv...) // later entries win
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var out bytes.Buffer
	w := &capWriter{buf: &out, max: MaxOutput * 2}
	cmd.Stdout, cmd.Stderr = w, w

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return "", err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err = <-done:
	case <-ctx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		err = ctx.Err()
	}
	res := out.String()
	if w.dropped > 0 {
		res += fmt.Sprintf("\n… [%d bytes of output dropped]", w.dropped)
	}
	dur := time.Since(start).Round(time.Millisecond)
	var exitErr *exec.ExitError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return res, fmt.Errorf("timed out after %s", timeout)
	case errors.Is(err, context.Canceled):
		return res, errors.New("interrupted")
	case errors.As(err, &exitErr):
		return res + fmt.Sprintf("\n[exit %d after %s]", exitErr.ExitCode(), dur), fmt.Errorf("exit status %d", exitErr.ExitCode())
	case err != nil:
		return res, err
	}
	return res + fmt.Sprintf("\n[exit 0 after %s]", dur), nil
}

type capWriter struct {
	buf     *bytes.Buffer
	max     int
	dropped int
}

func (c *capWriter) Write(p []byte) (int, error) {
	room := c.max - c.buf.Len()
	if room <= 0 {
		c.dropped += len(p)
		return len(p), nil
	}
	if len(p) > room {
		c.buf.Write(p[:room])
		c.dropped += len(p) - room
		return len(p), nil
	}
	return c.buf.Write(p)
}

// ---- http_fetch ----------------------------------------------------------------------------------

const maxFetchBody = 512 << 10

// ErrPrivateAddress blocks requests to internal networks (SSRF), including via redirects and DNS
// answers, because the check runs on the resolved IP at connect time.
var ErrPrivateAddress = errors.New("destination is a private, loopback or link-local address")

func safeDialer() *net.Dialer {
	return &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
				ip.IsUnspecified() || ip.IsMulticast() || isCGNAT(ip) {
				return ErrPrivateAddress
			}
			return nil
		},
	}
}

func isCGNAT(ip net.IP) bool {
	_, cgnat, _ := net.ParseCIDR("100.64.0.0/10")
	return cgnat.Contains(ip)
}

var fetchClient = &http.Client{
	Timeout: 20 * time.Second,
	Transport: &http.Transport{
		DialContext:         safeDialer().DialContext,
		Proxy:               nil, // a proxy would bypass the address check
		TLSHandshakeTimeout: 10 * time.Second,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
			return errors.New("redirect to a non-HTTP scheme")
		}
		return nil
	},
}

func httpFetch(ctx context.Context, in proto.HTTPFetchInput) (string, error) {
	method := strings.ToUpper(in.Method)
	if method == "" {
		method = http.MethodGet
	}
	if method != http.MethodGet && method != http.MethodHead {
		return "", errors.New("only GET and HEAD are allowed")
	}
	req, err := http.NewRequestWithContext(ctx, method, in.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "akili-agent")
	resp, err := fetchClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", resp.Proto, resp.Status)
	for _, h := range []string{"Content-Type", "Content-Length", "Location", "Server", "Date", "Last-Modified"} {
		if v := resp.Header.Get(h); v != "" {
			fmt.Fprintf(&b, "%s: %s\n", h, v)
		}
	}
	if method == http.MethodHead {
		return b.String(), nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBody+1))
	if err != nil {
		return b.String(), err
	}
	b.WriteString("\n")
	if isBinary(body) {
		fmt.Fprintf(&b, "[binary body, %d bytes]", len(body))
		return b.String(), nil
	}
	truncated := len(body) > maxFetchBody
	if truncated {
		body = body[:maxFetchBody]
	}
	b.Write(body)
	if truncated {
		b.WriteString("\n… [truncated]")
	}
	return b.String(), nil
}

// diskUsage reports total and free bytes of the filesystem holding path.
func diskUsage(path string) (total, free uint64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	return st.Blocks * uint64(st.Bsize), st.Bavail * uint64(st.Bsize), nil
}
