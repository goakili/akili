// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package tools

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/goakili/akili/proto"
)

// Project is the workspace of a project session.
type Project struct {
	Spec   proto.ProjectSpec
	Dir    string // the worktree, on Spec.Branch
	Mirror string // bare repository shared by the project's worktrees
	Cache  string // build caches mounted into the sandbox
	// Remote is the git proxy URL for this session (credentials live on the control plane).
	Remote string
}

// PrepareProject creates or refreshes the session's worktree. It only ever appends to the mirror
// (fetches); a worktree left by an earlier attempt of the same task is reused as is.
func PrepareProject(ctx context.Context, workdir string, spec proto.ProjectSpec, remote string) (*Project, error) {
	p := &Project{Spec: spec, Dir: proto.ProjectDir(workdir, spec.Slug, spec.Branch), Mirror: proto.ProjectMirror(workdir, spec.Slug),
		Cache: filepath.Join(workdir, "projects", spec.Slug, ".cache"), Remote: remote}
	if err := os.MkdirAll(filepath.Dir(p.Mirror), 0o750); err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(p.Mirror, "HEAD")); err != nil {
		if _, err := p.git(ctx, "", "init", "--bare", "--initial-branch="+spec.DefaultBranch, p.Mirror); err != nil {
			return nil, err
		}
	}
	steps := [][]string{
		{"config", "remote.origin.url", remote},
		// Remote-tracking refs, so a fetch never collides with a branch checked out in a worktree.
		{"config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*"},
		{"config", "user.name", spec.GitName},
		{"config", "user.email", spec.GitEmail},
		{"config", "core.hooksPath", "/dev/null"},
		{"config", "submodule.recurse", "false"},
		{"config", "protocol.file.allow", "never"},
		{"fetch", "--prune", "origin"},
	}
	for _, args := range steps {
		if _, err := p.git(ctx, p.Mirror, args...); err != nil {
			return nil, err
		}
	}
	if _, err := os.Stat(filepath.Join(p.Dir, ".git")); err == nil {
		return p, nil // resumed session or retried task: keep the work in progress
	}
	start := ""
	switch {
	case p.refExists(ctx, "refs/remotes/origin/"+spec.Branch):
		start = "origin/" + spec.Branch // the branch was pushed by an earlier attempt
	case p.refExists(ctx, "refs/remotes/origin/"+spec.DefaultBranch):
		start = "origin/" + spec.DefaultBranch
	}
	args := []string{"worktree", "add", "--force"}
	if start == "" {
		args = append(args, "--orphan", "-b", spec.Branch, p.Dir) // empty repository
	} else {
		args = append(args, "-B", spec.Branch, p.Dir, start)
	}
	if _, err := p.git(ctx, p.Mirror, args...); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Project) refExists(ctx context.Context, ref string) bool {
	_, err := p.git(ctx, p.Mirror, "rev-parse", "--verify", "--quiet", ref)
	return err == nil
}

// git runs git with a scrubbed environment: no system config, no prompts, no credential helpers.
func (p *Project) git(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = []string{
		"PATH=/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin",
		"HOME=" + filepath.Dir(p.Mirror),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_ASKPASS=/bin/false",
		"LANG=C.UTF-8",
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	if err != nil {
		return out.String(), fmt.Errorf("git %s: %v: %s", args[len(args)-min(len(args), 3)], err, strings.TrimSpace(out.String()))
	}
	return out.String(), nil
}

func (e *Executor) gitTool(ctx context.Context, tool string, input json.RawMessage) (string, error) {
	p := e.Project
	if p == nil {
		return "", errors.New("this session is not bound to a project")
	}
	switch tool {
	case proto.ToolGitStatus:
		st, err := p.git(ctx, p.Dir, "status", "--short", "--branch")
		if err != nil {
			return "", err
		}
		ahead, _ := p.git(ctx, p.Dir, "log", "--oneline", "origin/"+p.Spec.DefaultBranch+"..HEAD")
		unpushed, _ := p.git(ctx, p.Dir, "log", "--oneline", "origin/"+p.Spec.Branch+"..HEAD")
		var b strings.Builder
		b.WriteString(st)
		fmt.Fprintf(&b, "\nCommits on %s not in %s:\n%s", p.Spec.Branch, p.Spec.DefaultBranch, orNone(ahead))
		if p.refExists(ctx, "refs/remotes/origin/"+p.Spec.Branch) {
			fmt.Fprintf(&b, "\nCommits not pushed yet:\n%s", orNone(unpushed))
		} else {
			b.WriteString("\nThe branch has not been pushed yet.\n")
		}
		return b.String(), nil
	case proto.ToolGitDiff:
		var in proto.GitDiffInput
		_ = json.Unmarshal(input, &in)
		// Mark new files as intent-to-add so they appear in the diff; commit stages everything anyway.
		if !in.Staged {
			_, _ = p.git(ctx, p.Dir, "add", "--intent-to-add", "--all")
		}
		args := []string{"diff", "--stat", "--patch"}
		if in.Staged {
			args = append(args, "--staged")
		}
		if in.Path != "" {
			path, err := e.resolve(in.Path)
			if err != nil {
				return "", err
			}
			args = append(args, "--", path)
		}
		out, err := p.git(ctx, p.Dir, args...)
		if err == nil && strings.TrimSpace(out) == "" {
			return "no changes", nil
		}
		return out, err
	case proto.ToolGitCommit:
		var in proto.GitCommitInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		if _, err := p.git(ctx, p.Dir, "add", "--all"); err != nil {
			return "", err
		}
		if st, _ := p.git(ctx, p.Dir, "status", "--porcelain"); strings.TrimSpace(st) == "" {
			return "", errors.New("nothing to commit: the working tree is clean")
		}
		args := []string{"commit", "--no-verify", "--quiet", "-m", in.Message}
		for _, t := range p.Spec.Trailers {
			args = append(args, "--trailer", t)
		}
		if _, err := p.git(ctx, p.Dir, args...); err != nil {
			return "", err
		}
		return p.git(ctx, p.Dir, "show", "--stat", "--oneline", "--no-patch", "HEAD")
	case proto.ToolGitPush:
		// Only the session's branch, and never forced; the control plane enforces the same rule.
		return p.git(ctx, p.Dir, "push", "--porcelain", "origin", "HEAD:refs/heads/"+p.Spec.Branch)
	}
	return "", fmt.Errorf("unknown git tool %s", tool)
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(none)\n"
	}
	return s
}

const (
	defaultSandboxTimeout = 10 * time.Minute
	maxSandboxTimeout     = 30 * time.Minute
)

// sandboxExec runs a command in a disposable container: the worktree is the only host path it sees
// (plus a per-project build cache), capabilities are dropped and it runs as the agent's user.
func (e *Executor) sandboxExec(ctx context.Context, in proto.SandboxExecInput) (string, error) {
	p := e.Project
	if p == nil || p.Spec.SandboxImage == "" {
		return "", errors.New("this project has no sandbox image configured")
	}
	docker, err := exec.LookPath("docker")
	if err != nil {
		return "", errors.New("docker is not available on this agent host; sandbox_exec needs it")
	}
	if err := os.MkdirAll(p.Cache, 0o750); err != nil {
		return "", err
	}
	timeout := defaultSandboxTimeout
	if in.TimeoutSec > 0 {
		timeout = min(time.Duration(in.TimeoutSec)*time.Second, maxSandboxTimeout)
	}
	name := "akili-sbx-" + strings.ToLower(rand.Text()[:12])
	args := []string{"run", "--rm", "--name", name,
		"--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=1024", "--memory=4g", "--cpus=2",
		"--user", strconv.Itoa(os.Getuid()) + ":" + strconv.Itoa(os.Getgid()),
		"-v", p.Dir + ":/workspace", "-v", p.Cache + ":/cache", "-w", "/workspace",
		"-e", "HOME=/cache/home", "-e", "GOCACHE=/cache/go-build", "-e", "GOMODCACHE=/cache/gomod", "-e", "GOFLAGS=-buildvcs=false",
		"-e", "npm_config_cache=/cache/npm", "-e", "PIP_CACHE_DIR=/cache/pip", "-e", "CARGO_HOME=/cache/cargo", "-e", "CI=true",
		p.Spec.SandboxImage, "sh", "-c", "mkdir -p /cache/home && " + in.Command}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.Command(docker, args...)
	cmd.Env = append(os.Environ()[:0:0], "PATH="+os.Getenv("PATH"), "HOME="+os.Getenv("HOME"), "DOCKER_HOST="+os.Getenv("DOCKER_HOST"))
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
		_ = exec.Command(docker, "rm", "-f", name).Run() // stop the container, not just the client
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
