// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package forge

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Gitea implements Forge for Gitea (and Forgejo).
type Gitea struct {
	c        *client
	base     string
	username string
	token    string
}

// NewGitea returns a Gitea client. base is the instance URL (https://gitea.example.com).
func NewGitea(base, username, token string) *Gitea {
	base = strings.TrimRight(base, "/")
	return &Gitea{
		c: newClient(base+"/api/v1", func(_ context.Context, h http.Header) error {
			h.Set("Authorization", "token "+token)
			return nil
		}, nil),
		base: base, username: username, token: token,
	}
}

type giteaRepo struct {
	Name          string `json:"name"`
	DefaultBranch string `json:"default_branch"`
	HTMLURL       string `json:"html_url"`
	Private       bool   `json:"private"`
	Empty         bool   `json:"empty"`
	Owner         struct {
		Login string `json:"login"`
	} `json:"owner"`
}

func (r giteaRepo) repo() *Repo {
	return &Repo{Owner: r.Owner.Login, Name: r.Name, DefaultBranch: r.DefaultBranch, WebURL: r.HTMLURL, Private: r.Private, Empty: r.Empty}
}

type giteaPR struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
	State   string `json:"state"`
	Merged  bool   `json:"merged"`
	Title   string `json:"title"`
	Draft   bool   `json:"draft"`
	Head    struct {
		Ref string `json:"ref"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

func (p giteaPR) pr() *PR {
	return &PR{Number: p.Number, URL: p.HTMLURL, State: p.State, Merged: p.Merged, Title: p.Title, Head: p.Head.Ref, Base: p.Base.Ref, Draft: p.Draft}
}

func esc(s string) string { return url.PathEscape(s) }

// Verify implements Forge.
func (g *Gitea) Verify(ctx context.Context) (string, error) {
	var u struct {
		Login string `json:"login"`
	}
	if err := g.c.do(ctx, http.MethodGet, "/user", nil, &u); err != nil {
		return "", err
	}
	return u.Login, nil
}

// GetRepo implements Forge.
func (g *Gitea) GetRepo(ctx context.Context, owner, name string) (*Repo, error) {
	var r giteaRepo
	if err := g.c.do(ctx, http.MethodGet, "/repos/"+esc(owner)+"/"+esc(name), nil, &r); err != nil {
		return nil, err
	}
	return r.repo(), nil
}

// CreateRepo implements Forge.
func (g *Gitea) CreateRepo(ctx context.Context, owner, name, description string, private bool) (*Repo, error) {
	body := map[string]any{"name": name, "description": description, "private": private, "auto_init": true, "default_branch": "main", "readme": "Default"}
	path := "/user/repos"
	if owner != "" && !strings.EqualFold(owner, g.username) {
		path = "/orgs/" + esc(owner) + "/repos"
	}
	var r giteaRepo
	if err := g.c.do(ctx, http.MethodPost, path, body, &r); err != nil {
		return nil, err
	}
	return r.repo(), nil
}

// FindOpenPR implements Forge.
func (g *Gitea) FindOpenPR(ctx context.Context, owner, name, head string) (*PR, error) {
	var prs []giteaPR
	if err := g.c.do(ctx, http.MethodGet, "/repos/"+esc(owner)+"/"+esc(name)+"/pulls?state=open&limit=50", nil, &prs); err != nil {
		return nil, err
	}
	for _, p := range prs {
		if p.Head.Ref == head {
			return p.pr(), nil
		}
	}
	return nil, ErrNotFound
}

// CreatePR implements Forge. Gitea marks drafts with a "WIP:" title prefix.
func (g *Gitea) CreatePR(ctx context.Context, owner, name, head, base, title, body string, draft bool) (*PR, error) {
	if draft && !strings.HasPrefix(title, "WIP:") {
		title = "WIP: " + title
	}
	var p giteaPR
	err := g.c.do(ctx, http.MethodPost, "/repos/"+esc(owner)+"/"+esc(name)+"/pulls",
		map[string]any{"head": head, "base": base, "title": title, "body": body}, &p)
	if err != nil {
		return nil, err
	}
	return p.pr(), nil
}

// GetPR implements Forge.
func (g *Gitea) GetPR(ctx context.Context, owner, name string, number int) (*PR, error) {
	var p giteaPR
	if err := g.c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/%s/pulls/%d", esc(owner), esc(name), number), nil, &p); err != nil {
		return nil, err
	}
	return p.pr(), nil
}

// CommitStatus implements Forge (Gitea Actions and external CI both report commit statuses).
func (g *Gitea) CommitStatus(ctx context.Context, owner, name, ref string) (*Status, error) {
	var st struct {
		SHA      string `json:"sha"`
		Statuses []struct {
			Context     string `json:"context"`
			Status      string `json:"status"`
			Description string `json:"description"`
			TargetURL   string `json:"target_url"`
		} `json:"statuses"`
	}
	if err := g.c.do(ctx, http.MethodGet, "/repos/"+esc(owner)+"/"+esc(name)+"/commits/"+esc(ref)+"/status", nil, &st); err != nil {
		return nil, err
	}
	out := &Status{SHA: st.SHA, Checks: []Check{}}
	for _, s := range st.Statuses {
		state := s.Status
		if state == "warning" {
			state = "success"
		}
		out.Checks = append(out.Checks, Check{Name: s.Context, State: state, Description: s.Description, URL: s.TargetURL})
	}
	out.State = combine(out.Checks)
	return out, nil
}

// PRDiff implements Forge.
func (g *Gitea) PRDiff(ctx context.Context, owner, name string, number int) (string, error) {
	var diff string
	err := g.c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/%s/pulls/%d.diff", esc(owner), esc(name), number), nil, &diff, "text/plain")
	return diff, err
}

// GitAuth implements Forge.
func (g *Gitea) GitAuth(context.Context) (string, string, error) {
	user := g.username
	if user == "" {
		user = "akili"
	}
	return user, g.token, nil
}

// GitURL implements Forge.
func (g *Gitea) GitURL(owner, name string) string { return g.base + "/" + owner + "/" + name + ".git" }
