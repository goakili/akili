// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package tools

import (
	"context"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/goakili/akili/proto"
)

// sessionRemote serves one upstream repository at /<session>/repo.git, like the control plane's
// git proxy, and records which session each push came in as.
func sessionRemote(t *testing.T) (base string, pushes func() []string) {
	t.Helper()
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	up := filepath.Join(root, "repo.git")
	seed := filepath.Join(root, "seed")
	for _, args := range [][]string{
		{"init", "-q", "--bare", "--initial-branch=main", up},
		{"-C", up, "config", "http.receivepack", "true"},
		{"init", "-q", "--initial-branch=main", seed},
		{"-C", seed, "-c", "user.name=t", "-c", "user.email=t@e2e.local", "commit", "-q", "--allow-empty", "-m", "init"},
		{"-C", seed, "push", "-q", up, "main"},
	} {
		if out, err := exec.Command(gitBin, args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	var mu sync.Mutex
	var seen []string
	backend := &cgi.Handler{Path: gitBin, Args: []string{"http-backend"}, Env: []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, rest, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
		if r.Method == http.MethodPost && strings.HasSuffix(rest, "git-receive-pack") {
			mu.Lock()
			seen = append(seen, session)
			mu.Unlock()
		}
		r.URL.Path = "/" + rest
		backend.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

func spec(branch string) proto.ProjectSpec {
	return proto.ProjectSpec{Slug: "site", Repo: "o/site", DefaultBranch: "main", Branch: branch, GitName: "Agent", GitEmail: "agent@e2e.local"}
}

// Two sessions on one project share the mirror. Each must push as itself: the proxy only lets a
// session push its own branch, so a push sent under the other session's URL would be refused.
func TestSessionsPushAsThemselves(t *testing.T) {
	base, pushes := sessionRemote(t)
	ctx := context.Background()
	workdir := t.TempDir()

	a, err := PrepareProject(ctx, workdir, spec("akili/tsk_a"), base+"/ses_a/repo.git")
	if err != nil {
		t.Fatal(err)
	}
	// Session b opens later, as the second task on the same project did.
	b, err := PrepareProject(ctx, workdir, spec("akili/tsk_b"), base+"/ses_b/repo.git")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []*Project{a, b} {
		if _, err := p.git(ctx, p.Dir, "-c", "user.name=Agent", "-c", "user.email=agent@e2e.local", "commit", "-q", "--allow-empty", "-m", "work"); err != nil {
			t.Fatal(err)
		}
		if out, err := p.git(ctx, p.Dir, "push", "--porcelain", "origin", "HEAD:refs/heads/"+p.Spec.Branch); err != nil {
			t.Fatalf("push %s: %v %s", p.Spec.Branch, err, out)
		}
	}
	if got := pushes(); len(got) != 2 || got[0] != "ses_a" || got[1] != "ses_b" {
		t.Fatalf("pushes arrived as %v, want [ses_a ses_b]", got)
	}
	if out, _ := a.git(ctx, a.Mirror, "config", "--get-all", "remote.origin.url"); strings.TrimSpace(out) != "" {
		t.Fatalf("the shared mirror config still has a push URL: %q", out)
	}
	if out, _ := a.git(ctx, a.Dir, "rev-parse", "--is-bare-repository"); strings.TrimSpace(out) != "false" {
		t.Fatalf("a worktree became bare: %q", out)
	}

	// A resumed session (a retried task) gets its own URL back, even after another session opened.
	a2, err := PrepareProject(ctx, workdir, spec("akili/tsk_a"), base+"/ses_a2/repo.git")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a2.git(ctx, a2.Dir, "-c", "user.name=Agent", "-c", "user.email=agent@e2e.local", "commit", "-q", "--allow-empty", "-m", "more"); err != nil {
		t.Fatal(err)
	}
	if out, err := a2.git(ctx, a2.Dir, "push", "--porcelain", "origin", "HEAD:refs/heads/akili/tsk_a"); err != nil {
		t.Fatalf("push after resume: %v %s", err, out)
	}
	if got := pushes(); got[len(got)-1] != "ses_a2" {
		t.Fatalf("the resumed session pushed as %s", got[len(got)-1])
	}
}

// Mirrors made before per-worktree remotes kept one URL in the shared config; they are converted.
func TestOldMirrorIsConverted(t *testing.T) {
	base, pushes := sessionRemote(t)
	ctx := context.Background()
	workdir := t.TempDir()
	mirror := proto.ProjectMirror(workdir, "site")
	for _, args := range [][]string{
		{"init", "-q", "--bare", "--initial-branch=main", mirror},
		{"-C", mirror, "config", "remote.origin.url", base + "/ses_old/repo.git"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	p, err := PrepareProject(ctx, workdir, spec("akili/tsk_new"), base+"/ses_new/repo.git")
	if err != nil {
		t.Fatal(err)
	}
	if out, err := p.git(ctx, p.Dir, "push", "--porcelain", "origin", "HEAD:refs/heads/akili/tsk_new"); err != nil {
		t.Fatalf("push: %v %s", err, out)
	}
	if got := pushes(); len(got) != 1 || got[0] != "ses_new" {
		t.Fatalf("pushes arrived as %v, want [ses_new] only", got)
	}
}

// Sessions of one project open at the same time (a task and a chat); preparing must not race on the
// mirror's config lock.
func TestConcurrentPrepare(t *testing.T) {
	base, _ := sessionRemote(t)
	ctx := context.Background()
	workdir := t.TempDir()
	var wg sync.WaitGroup
	errs := make(chan error, 6)
	for i := range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := PrepareProject(ctx, workdir, spec("akili/tsk_"+string(rune('a'+i))), base+"/ses_"+string(rune('a'+i))+"/repo.git")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}
