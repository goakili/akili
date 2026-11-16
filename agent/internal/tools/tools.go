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

// Package tools executes catalog tools on the host, adding symlink-resolved policy checks and output limits.
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

type Executor struct {
	Workdir string
	Policy  proto.Policy
	Facts   func() proto.HostFacts
	// ShellEnv holds only variables named in AKILI_AGENT_SHELL_ENV; they override the defaults (PATH, HOME, ...).
	ShellEnv []string
	// Project is set in project sessions: relative paths resolve in its worktree.
	Project *Project
	// Protected dirs hold the agent's private key and are refused whatever the policy, unless inside the workdir or project.
	Protected []string
}

// base is where relative paths resolve.
func (e *Executor) base() string {
	if e.Project != nil {
		return e.Project.Dir
	}
	return e.Workdir
}

type Result struct {
	Output    string
	IsError   bool
	Truncated bool
}

// Run expects input already validated by proto.Evaluate; output is redacted before it reaches the model.
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

// resolve re-checks the policy on the symlink-resolved path, or on the nearest existing parent for a new path.
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
