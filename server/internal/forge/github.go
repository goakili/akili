// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package forge

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// GitHub implements Forge for github.com and GitHub Enterprise Server.
type GitHub struct {
	c      *client
	webURL string
	tokens tokenSource
}

type tokenSource interface {
	Token(ctx context.Context) (string, error)
	Kind() string
}

// NewGitHubToken authenticates with a personal or fine-grained access token.
func NewGitHubToken(apiURL, webURL, token string) *GitHub {
	return newGitHub(apiURL, webURL, staticToken(token))
}

// NewGitHubApp authenticates as a GitHub App installation. Installation tokens are minted on demand
// and expire after an hour, so a leaked token is short-lived.
func NewGitHubApp(apiURL, webURL string, appID, installationID int64, privateKeyPEM []byte) (*GitHub, error) {
	key, err := parseRSAKey(privateKeyPEM)
	if err != nil {
		return nil, err
	}
	g := newGitHub(apiURL, webURL, nil)
	g.tokens = &appTokens{api: newClient(apiURL, nil, githubHeaders), appID: appID, installationID: installationID, key: key}
	return g, nil
}

var githubHeaders = map[string]string{"X-GitHub-Api-Version": "2022-11-28"}

func newGitHub(apiURL, webURL string, ts tokenSource) *GitHub {
	if apiURL == "" {
		apiURL = "https://api.github.com"
	}
	if webURL == "" {
		webURL = "https://github.com"
	}
	g := &GitHub{webURL: strings.TrimRight(webURL, "/"), tokens: ts}
	g.c = newClient(apiURL, func(ctx context.Context, h http.Header) error {
		t, err := g.tokens.Token(ctx)
		if err != nil {
			return err
		}
		h.Set("Authorization", "Bearer "+t)
		return nil
	}, githubHeaders)
	return g
}

type staticToken string

func (s staticToken) Token(context.Context) (string, error) { return string(s), nil }
func (staticToken) Kind() string                            { return "token" }

type appTokens struct {
	api            *client
	appID          int64
	installationID int64
	key            *rsa.PrivateKey

	mu      sync.Mutex
	token   string
	expires time.Time
}

func (a *appTokens) Kind() string { return "app" }

func (a *appTokens) Token(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.token != "" && time.Until(a.expires) > 5*time.Minute {
		return a.token, nil
	}
	now := time.Now()
	appJWT, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(now.Add(-60 * time.Second)), // tolerate clock drift
		ExpiresAt: jwt.NewNumericDate(now.Add(9 * time.Minute)),
		Issuer:    fmt.Sprint(a.appID),
	}).SignedString(a.key)
	if err != nil {
		return "", err
	}
	var out struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	c := *a.api
	c.auth = func(_ context.Context, h http.Header) error {
		h.Set("Authorization", "Bearer "+appJWT)
		return nil
	}
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/app/installations/%d/access_tokens", a.installationID), nil, &out); err != nil {
		return "", fmt.Errorf("github app token: %w", err)
	}
	a.token, a.expires = out.Token, out.ExpiresAt
	return a.token, nil
}

func parseRSAKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("github app private key: not PEM")
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("github app private key: %w", err)
	}
	rk, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("github app private key: not RSA")
	}
	return rk, nil
}

type githubRepo struct {
	Name          string `json:"name"`
	DefaultBranch string `json:"default_branch"`
	HTMLURL       string `json:"html_url"`
	Private       bool   `json:"private"`
	Size          int    `json:"size"`
	Owner         struct {
		Login string `json:"login"`
	} `json:"owner"`
}

func (r githubRepo) repo() *Repo {
	return &Repo{Owner: r.Owner.Login, Name: r.Name, DefaultBranch: r.DefaultBranch, WebURL: r.HTMLURL, Private: r.Private}
}

type githubPR struct {
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

func (p githubPR) pr() *PR {
	return &PR{Number: p.Number, URL: p.HTMLURL, State: p.State, Merged: p.Merged, Title: p.Title, Head: p.Head.Ref, Base: p.Base.Ref, Draft: p.Draft}
}

// Verify implements Forge.
func (g *GitHub) Verify(ctx context.Context) (string, error) {
	if g.tokens.Kind() == "app" {
		var out struct {
			TotalCount int `json:"total_count"`
		}
		if err := g.c.do(ctx, http.MethodGet, "/installation/repositories?per_page=1", nil, &out); err != nil {
			return "", err
		}
		return fmt.Sprintf("GitHub App installation (%d repositories)", out.TotalCount), nil
	}
	var u struct {
		Login string `json:"login"`
	}
	if err := g.c.do(ctx, http.MethodGet, "/user", nil, &u); err != nil {
		return "", err
	}
	return u.Login, nil
}

// GetRepo implements Forge.
func (g *GitHub) GetRepo(ctx context.Context, owner, name string) (*Repo, error) {
	var r githubRepo
	if err := g.c.do(ctx, http.MethodGet, "/repos/"+esc(owner)+"/"+esc(name), nil, &r); err != nil {
		return nil, err
	}
	return r.repo(), nil
}

// CreateRepo implements Forge.
func (g *GitHub) CreateRepo(ctx context.Context, owner, name, description string, private bool) (*Repo, error) {
	body := map[string]any{"name": name, "description": description, "private": private, "auto_init": true}
	path := "/user/repos"
	if owner != "" {
		if who, err := g.Verify(ctx); err != nil || !strings.EqualFold(who, owner) {
			path = "/orgs/" + esc(owner) + "/repos"
		}
	}
	var r githubRepo
	if err := g.c.do(ctx, http.MethodPost, path, body, &r); err != nil {
		return nil, err
	}
	return r.repo(), nil
}

// FindOpenPR implements Forge.
func (g *GitHub) FindOpenPR(ctx context.Context, owner, name, head string) (*PR, error) {
	var prs []githubPR
	if err := g.c.do(ctx, http.MethodGet, "/repos/"+esc(owner)+"/"+esc(name)+"/pulls?state=open&head="+esc(owner+":"+head), nil, &prs); err != nil {
		return nil, err
	}
	if len(prs) == 0 {
		return nil, ErrNotFound
	}
	return prs[0].pr(), nil
}

// CreatePR implements Forge.
func (g *GitHub) CreatePR(ctx context.Context, owner, name, head, base, title, body string, draft bool) (*PR, error) {
	var p githubPR
	err := g.c.do(ctx, http.MethodPost, "/repos/"+esc(owner)+"/"+esc(name)+"/pulls",
		map[string]any{"head": head, "base": base, "title": title, "body": body, "draft": draft}, &p)
	if err != nil {
		return nil, err
	}
	return p.pr(), nil
}

// GetPR implements Forge.
func (g *GitHub) GetPR(ctx context.Context, owner, name string, number int) (*PR, error) {
	var p githubPR
	if err := g.c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/%s/pulls/%d", esc(owner), esc(name), number), nil, &p); err != nil {
		return nil, err
	}
	return p.pr(), nil
}

// CommitStatus implements Forge, merging commit statuses and check runs (GitHub Actions uses the latter).
func (g *GitHub) CommitStatus(ctx context.Context, owner, name, ref string) (*Status, error) {
	base := "/repos/" + esc(owner) + "/" + esc(name) + "/commits/" + esc(ref)
	var combined struct {
		SHA      string `json:"sha"`
		Statuses []struct {
			Context     string `json:"context"`
			State       string `json:"state"`
			Description string `json:"description"`
			TargetURL   string `json:"target_url"`
		} `json:"statuses"`
	}
	if err := g.c.do(ctx, http.MethodGet, base+"/status", nil, &combined); err != nil {
		return nil, err
	}
	out := &Status{SHA: combined.SHA, Checks: []Check{}}
	for _, s := range combined.Statuses {
		out.Checks = append(out.Checks, Check{Name: s.Context, State: s.State, Description: s.Description, URL: s.TargetURL})
	}
	var runs struct {
		CheckRuns []struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			HTMLURL    string `json:"html_url"`
		} `json:"check_runs"`
	}
	if err := g.c.do(ctx, http.MethodGet, base+"/check-runs?per_page=100", nil, &runs); err == nil {
		for _, r := range runs.CheckRuns {
			state := "pending"
			if r.Status == "completed" {
				switch r.Conclusion {
				case "success", "neutral", "skipped":
					state = "success"
				default:
					state = "failure"
				}
			}
			out.Checks = append(out.Checks, Check{Name: r.Name, State: state, Description: r.Conclusion, URL: r.HTMLURL})
		}
	}
	out.State = combine(out.Checks)
	return out, nil
}

// PRDiff implements Forge.
func (g *GitHub) PRDiff(ctx context.Context, owner, name string, number int) (string, error) {
	var diff string
	err := g.c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/%s/pulls/%d", esc(owner), esc(name), number), nil, &diff, "application/vnd.github.diff")
	return diff, err
}

// GitAuth implements Forge.
func (g *GitHub) GitAuth(ctx context.Context) (string, string, error) {
	t, err := g.tokens.Token(ctx)
	return "x-access-token", t, err
}

// GitURL implements Forge.
func (g *GitHub) GitURL(owner, name string) string {
	return g.webURL + "/" + owner + "/" + name + ".git"
}
