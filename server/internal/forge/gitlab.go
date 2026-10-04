// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package forge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/goakili/akili/server/internal/crypto"
)

// GitLab implements Forge for gitlab.com and self-managed GitLab (API v4). Owners are namespace
// paths and may be nested groups (platform/backend); every call addresses a project by its
// URL-encoded path, so GitLab's numeric ids are never stored.
type GitLab struct {
	c         *client
	webURL    string
	token     string
	transport http.RoundTripper
}

// GitLab access levels (members API).
const (
	GitLabDeveloper  = 30
	GitLabMaintainer = 40
	GitLabOwner      = 50
)

// NewGitLab returns a GitLab client. base is the instance URL (https://gitlab.com); webURL defaults
// to it. caPEM is an extra CA to trust, for a self-managed instance on a private CA.
func NewGitLab(base, webURL, token, caPEM string) (*GitLab, error) {
	base = strings.TrimSuffix(strings.TrimRight(base, "/"), "/api/v4")
	if base == "" {
		base = "https://gitlab.com"
	}
	if webURL == "" {
		webURL = base
	}
	g := &GitLab{webURL: strings.TrimRight(webURL, "/"), token: token}
	g.c = newClient(base+"/api/v4", func(_ context.Context, h http.Header) error {
		h.Set("Authorization", "Bearer "+token)
		return nil
	}, nil)
	if strings.TrimSpace(caPEM) != "" {
		tlsCfg, err := crypto.TLSConfigWithCA(caPEM)
		if err != nil {
			return nil, err
		}
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.TLSClientConfig = tlsCfg
		g.transport = t
		g.c.http.Transport = t
	}
	return g, nil
}

// GitTransport is the transport git smart-HTTP must use to reach the instance (nil: the default).
func (g *GitLab) GitTransport() http.RoundTripper { return g.transport }

func (g *GitLab) project(owner, name string) string {
	return "/projects/" + url.PathEscape(owner+"/"+name)
}

type gitlabProject struct {
	Path          string `json:"path"`
	DefaultBranch string `json:"default_branch"`
	WebURL        string `json:"web_url"`
	Visibility    string `json:"visibility"`
	EmptyRepo     bool   `json:"empty_repo"`
	Namespace     struct {
		FullPath string `json:"full_path"`
	} `json:"namespace"`
}

func (p gitlabProject) repo() *Repo {
	return &Repo{Owner: p.Namespace.FullPath, Name: p.Path, DefaultBranch: p.DefaultBranch, WebURL: p.WebURL, Private: p.Visibility != "public", Empty: p.EmptyRepo}
}

type gitlabMR struct {
	IID          int    `json:"iid"`
	WebURL       string `json:"web_url"`
	State        string `json:"state"`
	Title        string `json:"title"`
	Draft        bool   `json:"draft"`
	WIP          bool   `json:"work_in_progress"` // GitLab before 14
	SourceBranch string `json:"source_branch"`
	TargetBranch string `json:"target_branch"`
}

func (m gitlabMR) pr() *PR {
	p := &PR{Number: m.IID, URL: m.WebURL, Title: m.Title, Head: m.SourceBranch, Base: m.TargetBranch, Draft: m.Draft || m.WIP, State: "closed"}
	switch m.State {
	case "opened":
		p.State = "open"
	case "merged":
		p.Merged = true
	}
	return p
}

// Verify implements Forge.
func (g *GitLab) Verify(ctx context.Context) (string, error) {
	u, err := g.user(ctx)
	if err != nil {
		return "", err
	}
	return u.Username, nil
}

type gitlabUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Bot      bool   `json:"bot"`
}

func (g *GitLab) user(ctx context.Context) (*gitlabUser, error) {
	var u gitlabUser
	if err := g.c.do(ctx, http.MethodGet, "/user", nil, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// TokenInfo describes the credentials of an integration.
type TokenInfo struct {
	// Kind is personal, project or group.
	Kind      string
	ExpiresAt *time.Time
}

var botUserRE = regexp.MustCompile(`^(project|group)_\d+_bot`)

// TokenInfo reports the token's kind and expiry; GitLab disables expired tokens without warning.
func (g *GitLab) TokenInfo(ctx context.Context) (*TokenInfo, error) {
	u, err := g.user(ctx)
	if err != nil {
		return nil, err
	}
	info := &TokenInfo{Kind: "personal"}
	if m := botUserRE.FindStringSubmatch(u.Username); u.Bot && m != nil {
		info.Kind = m[1]
	}
	var tok struct {
		ExpiresAt string `json:"expires_at"`
	}
	if err := g.c.do(ctx, http.MethodGet, "/personal_access_tokens/self", nil, &tok); err != nil {
		return info, err
	}
	if tok.ExpiresAt != "" {
		if t, err := time.Parse("2006-01-02", tok.ExpiresAt); err == nil {
			info.ExpiresAt = &t
		}
	}
	return info, nil
}

// AccessLevel is the token user's effective access level on a project (direct or inherited).
func (g *GitLab) AccessLevel(ctx context.Context, owner, name string) (int, error) {
	u, err := g.user(ctx)
	if err != nil {
		return 0, err
	}
	var m struct {
		AccessLevel int `json:"access_level"`
	}
	if err := g.c.do(ctx, http.MethodGet, fmt.Sprintf("%s/members/all/%d", g.project(owner, name), u.ID), nil, &m); err != nil {
		return 0, err
	}
	return m.AccessLevel, nil
}

// GetRepo implements Forge.
func (g *GitLab) GetRepo(ctx context.Context, owner, name string) (*Repo, error) {
	var p gitlabProject
	if err := g.c.do(ctx, http.MethodGet, g.project(owner, name), nil, &p); err != nil {
		return nil, err
	}
	return p.repo(), nil
}

// ErrProjectToken explains why a project access token can't create repositories.
var ErrProjectToken = errors.New("a GitLab project access token can't create projects; use a group access token with the Maintainer role, or a personal token")

// CreateRepo implements Forge. owner is a group path, or the token user's own namespace.
func (g *GitLab) CreateRepo(ctx context.Context, owner, name, description string, private bool) (*Repo, error) {
	visibility := "public"
	if private {
		visibility = "private"
	}
	body := map[string]any{"name": name, "path": name, "description": description, "visibility": visibility,
		"initialize_with_readme": true, "default_branch": "main"}
	if owner != "" {
		var ns struct {
			ID   int64  `json:"id"`
			Kind string `json:"kind"`
		}
		if err := g.c.do(ctx, http.MethodGet, "/namespaces/"+url.PathEscape(owner), nil, &ns); err != nil {
			return nil, fmt.Errorf("namespace %s: %w", owner, err)
		}
		if ns.Kind != "user" {
			body["namespace_id"] = ns.ID
		}
	}
	var p gitlabProject
	if err := g.c.do(ctx, http.MethodPost, "/projects", body, &p); err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusForbidden {
			return nil, ErrProjectToken
		}
		return nil, err
	}
	return p.repo(), nil
}

// FindOpenPR implements Forge.
func (g *GitLab) FindOpenPR(ctx context.Context, owner, name, head string) (*PR, error) {
	var mrs []gitlabMR
	q := url.Values{"source_branch": {head}, "state": {"opened"}, "per_page": {"20"}}
	if err := g.c.do(ctx, http.MethodGet, g.project(owner, name)+"/merge_requests?"+q.Encode(), nil, &mrs); err != nil {
		return nil, err
	}
	for _, m := range mrs {
		if m.SourceBranch == head {
			return m.pr(), nil
		}
	}
	return nil, ErrNotFound
}

// CreatePR implements Forge. GitLab marks drafts with a "Draft:" title prefix.
func (g *GitLab) CreatePR(ctx context.Context, owner, name, head, base, title, body string, draft bool) (*PR, error) {
	if draft && !strings.HasPrefix(title, "Draft:") {
		title = "Draft: " + title
	}
	var m gitlabMR
	err := g.c.do(ctx, http.MethodPost, g.project(owner, name)+"/merge_requests",
		map[string]any{"source_branch": head, "target_branch": base, "title": title, "description": body}, &m)
	if err != nil {
		return nil, err
	}
	return m.pr(), nil
}

// GetPR implements Forge.
func (g *GitLab) GetPR(ctx context.Context, owner, name string, number int) (*PR, error) {
	var m gitlabMR
	if err := g.c.do(ctx, http.MethodGet, fmt.Sprintf("%s/merge_requests/%d", g.project(owner, name), number), nil, &m); err != nil {
		return nil, err
	}
	return m.pr(), nil
}

// gitlabState maps a commit status to Akili's; "" leaves it out (manual and skipped jobs would hold
// every merge request pending forever).
func gitlabState(s string, allowFailure bool) string {
	switch s {
	case "success":
		return "success"
	case "created", "waiting_for_resource", "preparing", "pending", "running", "scheduled":
		return "pending"
	case "failed":
		if allowFailure {
			return "success"
		}
		return "failure"
	case "canceled":
		return "error"
	}
	return ""
}

// CommitStatus implements Forge. ref is a branch; GitLab CI and external CI both report commit statuses.
func (g *GitLab) CommitStatus(ctx context.Context, owner, name, ref string) (*Status, error) {
	var br struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	if err := g.c.do(ctx, http.MethodGet, g.project(owner, name)+"/repository/branches/"+url.PathEscape(ref), nil, &br); err != nil {
		return nil, err
	}
	var list []struct {
		ID           int64  `json:"id"`
		Name         string `json:"name"`
		Status       string `json:"status"`
		Description  string `json:"description"`
		TargetURL    string `json:"target_url"`
		AllowFailure bool   `json:"allow_failure"`
	}
	if err := g.c.do(ctx, http.MethodGet, g.project(owner, name)+"/repository/commits/"+url.PathEscape(br.Commit.ID)+"/statuses?per_page=100", nil, &list); err != nil {
		return nil, err
	}
	// A retried job reports again under the same name; the newest (highest id) wins.
	latest := map[string]int{}
	for i, s := range list {
		if j, ok := latest[s.Name]; !ok || s.ID > list[j].ID {
			latest[s.Name] = i
		}
	}
	out := &Status{SHA: br.Commit.ID, Checks: []Check{}}
	for i, s := range list {
		if latest[s.Name] != i {
			continue
		}
		state := gitlabState(s.Status, s.AllowFailure)
		if state == "" {
			continue
		}
		desc := s.Description
		if s.Status == "failed" && s.AllowFailure {
			desc = strings.TrimSpace("allowed to fail " + desc)
		}
		out.Checks = append(out.Checks, Check{Name: s.Name, State: state, Description: desc, URL: s.TargetURL})
	}
	out.State = combine(out.Checks)
	return out, nil
}

// PRDiff implements Forge. raw_diffs is GitLab 17.6+; older instances page through /diffs.
func (g *GitLab) PRDiff(ctx context.Context, owner, name string, number int) (string, error) {
	mr := fmt.Sprintf("%s/merge_requests/%d", g.project(owner, name), number)
	var diff string
	err := g.c.do(ctx, http.MethodGet, mr+"/raw_diffs", nil, &diff, "text/plain")
	if err == nil {
		return diff, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return "", err
	}
	var b strings.Builder
	for page := "1"; page != ""; {
		var files []struct {
			OldPath     string `json:"old_path"`
			NewPath     string `json:"new_path"`
			Diff        string `json:"diff"`
			NewFile     bool   `json:"new_file"`
			DeletedFile bool   `json:"deleted_file"`
		}
		h, err := g.c.doH(ctx, http.MethodGet, mr+"/diffs?per_page=100&page="+page, nil, &files)
		if err != nil {
			return "", err
		}
		for _, f := range files {
			from, to := "a/"+f.OldPath, "b/"+f.NewPath
			fmt.Fprintf(&b, "diff --git %s %s\n", from, to)
			if f.NewFile {
				from = "/dev/null"
				b.WriteString("new file mode 100644\n")
			}
			if f.DeletedFile {
				to = "/dev/null"
				b.WriteString("deleted file mode 100644\n")
			}
			fmt.Fprintf(&b, "--- %s\n+++ %s\n%s", from, to, f.Diff)
			if f.Diff != "" && !strings.HasSuffix(f.Diff, "\n") {
				b.WriteString("\n")
			}
		}
		page = h.Get("X-Next-Page")
		if n, err := strconv.Atoi(page); err != nil || n > 100 {
			page = ""
		}
	}
	return b.String(), nil
}

// GitAuth implements Forge: every GitLab token type authenticates git as oauth2.
func (g *GitLab) GitAuth(context.Context) (string, string, error) { return "oauth2", g.token, nil }

// GitURL implements Forge.
func (g *GitLab) GitURL(owner, name string) string {
	return g.webURL + "/" + owner + "/" + name + ".git"
}
