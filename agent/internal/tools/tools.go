// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package tools executes the catalog tools on the agent's host. Every call has already been allowed
// by the control plane; this package adds the checks only the host can make — resolving symlinks
// and re-checking the real path against the policy — plus output and time limits.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/goakili/akili/proto"
)

// MaxOutput bounds what one tool call returns to the model.
const MaxOutput = 64 << 10

// Executor runs tools under one policy.
type Executor struct {
	Workdir string
	Policy  proto.Policy
	Facts   func() proto.HostFacts
	// ShellEnv are extra KEY=value pairs added to the scrubbed shell environment. Only variables the
	// operator named in AKILI_AGENT_SHELL_ENV get here; they override the defaults (PATH, HOME, ...).
	ShellEnv []string
	// Project is set in project sessions: relative paths resolve in its worktree.
	Project *Project
	// Protected directories are refused whatever the policy says, except inside the workdir or the
	// project: they hold the agent's private key, which no template may expose.
	Protected []string
}

// base is where relative paths resolve.
func (e *Executor) base() string {
	if e.Project != nil {
		return e.Project.Dir
	}
	return e.Workdir
}

// Result is a tool outcome.
type Result struct {
	Output    string
	IsError   bool
	Truncated bool
}

// Run executes a tool. Input has already been validated by proto.Evaluate (strict decode).
// Output is redacted before it reaches the model: secret values the operator passed through
// AKILI_AGENT_SHELL_ENV and well-known credential formats.
func (e *Executor) Run(ctx context.Context, tool string, input json.RawMessage) Result {
	out, err := e.run(ctx, tool, input)
	out = proto.Redact(out, proto.SecretsFromEnv(e.ShellEnv)...)
	if err != nil {
		if out != "" {
			out += "\n"
		}
		out += "error: " + err.Error()
		return truncate(out, true)
	}
	return truncate(out, false)
}

func (e *Executor) run(ctx context.Context, tool string, input json.RawMessage) (string, error) {
	switch tool {
	case proto.ToolFSRead:
		var in proto.FSReadInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		return e.fsRead(in)
	case proto.ToolFSList:
		var in proto.FSListInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		return e.fsList(in)
	case proto.ToolFSWrite:
		var in proto.FSWriteInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		return e.fsWrite(in)
	case proto.ToolFSEdit:
		var in proto.FSEditInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		return e.fsEdit(in)
	case proto.ToolSearch:
		var in proto.SearchInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		return e.search(ctx, in)
	case proto.ToolShell:
		var in proto.ShellInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		return e.shell(ctx, in)
	case proto.ToolHTTPFetch:
		var in proto.HTTPFetchInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		return httpFetch(ctx, in)
	case proto.ToolHostInfo:
		return e.hostInfo()
	case proto.ToolGitStatus, proto.ToolGitDiff, proto.ToolGitCommit, proto.ToolGitPush:
		return e.gitTool(ctx, tool, input)
	case proto.ToolServiceStatus, proto.ToolServiceRestart, proto.ToolJournalLogs, proto.ToolDiskUsage, proto.ToolProcessList,
		proto.ToolDockerPS, proto.ToolDockerLogs, proto.ToolDockerRestart, proto.ToolCertCheck, proto.ToolPackageUpdates, proto.ToolNetProbe:
		return e.hostTool(ctx, tool, input)
	case proto.ToolSandboxExec:
		var in proto.SandboxExecInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		return e.sandboxExec(ctx, in)
	}
	return "", fmt.Errorf("tool %q is not implemented by this agent", tool)
}

// ErrPathDenied is returned when a path resolves (through symlinks) outside what the policy allows.
var ErrPathDenied = errors.New("path is not allowed by policy")

// resolve makes p absolute and follows symlinks, then re-checks the policy on the real path. For a
// path that does not exist yet, the nearest existing parent is resolved instead.
func (e *Executor) resolve(p string) (string, error) {
	abs := proto.ResolvePath(e.base(), p)
	real, err := realPath(abs)
	if err != nil {
		return "", err
	}
	if !e.Policy.PathAllowed(e.Workdir, abs) || !e.Policy.PathAllowed(e.Workdir, real) {
		return "", fmt.Errorf("%w: %s", ErrPathDenied, real)
	}
	if e.protected(real) {
		return "", fmt.Errorf("%w: %s is the agent's own state", ErrPathDenied, real)
	}
	return real, nil
}

func within(p, dir string) bool {
	return p == dir || strings.HasPrefix(p, strings.TrimSuffix(dir, string(filepath.Separator))+string(filepath.Separator))
}

func (e *Executor) protected(real string) bool {
	for _, d := range e.Protected {
		if d == "" {
			continue
		}
		pd, err := realPath(proto.ResolvePath("/", d))
		if err != nil || !within(real, pd) {
			continue
		}
		allowed := []string{e.Workdir}
		if e.Project != nil {
			allowed = append(allowed, e.Project.Dir)
		}
		inside := false
		for _, a := range allowed {
			if ra, err := realPath(a); err == nil && a != "" && within(real, ra) {
				inside = true
			}
		}
		if !inside {
			return true
		}
	}
	return false
}

func realPath(abs string) (string, error) {
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		return r, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	dir, rest := filepath.Dir(abs), filepath.Base(abs)
	for {
		if r, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(r, rest), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return abs, nil
		}
		rest = filepath.Join(filepath.Base(dir), rest)
		dir = parent
	}
}

func truncate(s string, isErr bool) Result {
	if len(s) <= MaxOutput {
		return Result{Output: s, IsError: isErr}
	}
	head := s[:MaxOutput*3/4]
	tail := s[len(s)-MaxOutput/4:]
	return Result{Output: head + fmt.Sprintf("\n… [%d bytes omitted] …\n", len(s)-len(head)-len(tail)) + tail, IsError: isErr, Truncated: true}
}

func isBinary(b []byte) bool {
	n := len(b)
	if n > 8000 {
		n = 8000
	}
	return strings.ContainsRune(string(b[:n]), 0)
}

func (e *Executor) hostInfo() (string, error) {
	f := e.Facts()
	var b strings.Builder
	fmt.Fprintf(&b, "hostname: %s\nos: %s/%s\nkernel: %s\ncpus: %d\n", f.Hostname, f.OS, f.Arch, f.Kernel, f.CPUs)
	if f.MemTotalMB > 0 {
		fmt.Fprintf(&b, "memory: %d MB total, %d MB available\n", f.MemTotalMB, f.MemAvailMB)
	}
	if f.Load1 > 0 {
		fmt.Fprintf(&b, "load1: %.2f\n", f.Load1)
	}
	if f.UptimeSec > 0 {
		fmt.Fprintf(&b, "uptime: %s\n", (time.Duration(f.UptimeSec) * time.Second).String())
	}
	if total, free, err := diskUsage(e.Workdir); err == nil {
		fmt.Fprintf(&b, "workdir %s: %.1f GB free of %.1f GB\n", e.Workdir, float64(free)/1e9, float64(total)/1e9)
	}
	fmt.Fprintf(&b, "agent: %s\n", f.AgentVersion)
	return b.String(), nil
}
