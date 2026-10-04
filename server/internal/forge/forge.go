// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package forge talks to git forges (Gitea, GitHub, GitLab). Only the control plane uses it: agents never
// hold forge credentials.
package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Repo is a repository.
type Repo struct {
	Owner         string `json:"owner"`
	Name          string `json:"name"`
	DefaultBranch string `json:"default_branch"`
	WebURL        string `json:"web_url"`
	Private       bool   `json:"private"`
	Empty         bool   `json:"empty"`
}

// PR is a pull request.
type PR struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	State  string `json:"state"` // open | closed
	Merged bool   `json:"merged"`
	Title  string `json:"title"`
	Head   string `json:"head"`
	Base   string `json:"base"`
	Draft  bool   `json:"draft"`
}

// Check is one CI status or check run.
type Check struct {
	Name        string `json:"name"`
	State       string `json:"state"` // success | pending | failure | error
	Description string `json:"description,omitempty"`
	URL         string `json:"url,omitempty"`
}

// Status is the combined CI state of a ref.
type Status struct {
	State  string  `json:"state"` // success | pending | failure | none
	SHA    string  `json:"sha,omitempty"`
	Checks []Check `json:"checks"`
}

// Forge is a git hosting service.
type Forge interface {
	// Verify checks the credentials and returns who they authenticate as.
	Verify(ctx context.Context) (string, error)
	GetRepo(ctx context.Context, owner, name string) (*Repo, error)
	// CreateRepo creates owner/name with an initial commit on main. owner may be the authenticated
	// user or an organization.
	CreateRepo(ctx context.Context, owner, name, description string, private bool) (*Repo, error)
	FindOpenPR(ctx context.Context, owner, name, head string) (*PR, error)
	CreatePR(ctx context.Context, owner, name, head, base, title, body string, draft bool) (*PR, error)
	GetPR(ctx context.Context, owner, name string, number int) (*PR, error)
	CommitStatus(ctx context.Context, owner, name, ref string) (*Status, error)
	PRDiff(ctx context.Context, owner, name string, number int) (string, error)
	// GitAuth returns HTTP basic credentials for git smart-HTTP.
	GitAuth(ctx context.Context) (user, password string, err error)
	// GitURL is the smart-HTTP base of a repository, e.g. https://host/owner/name.git.
	GitURL(owner, name string) string
}

// ErrNotFound is returned for missing repositories or pull requests.
var ErrNotFound = errors.New("not found")

// APIError is a non-2xx forge response.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("forge: %d: %s", e.Status, e.Message) }

// client is a small JSON HTTP client shared by the implementations.
type client struct {
	base string
	http *http.Client
	auth func(ctx context.Context, h http.Header) error
	// extra headers (e.g. GitHub API version).
	headers map[string]string
}

func newClient(base string, auth func(context.Context, http.Header) error, headers map[string]string) *client {
	return &client{base: strings.TrimRight(base, "/"), http: &http.Client{Timeout: 30 * time.Second}, auth: auth, headers: headers}
}

// do sends a request; out may be nil, or *string for a raw body.
func (c *client) do(ctx context.Context, method, path string, in, out any, accept ...string) error {
	_, err := c.doH(ctx, method, path, in, out, accept...)
	return err
}

// doH is do that also returns the response headers (for paging).
func (c *client) doH(ctx context.Context, method, path string, in, out any, accept ...string) (http.Header, error) {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return nil, err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if len(accept) > 0 {
		req.Header.Set("Accept", accept[0])
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	if c.auth != nil {
		if err := c.auth(ctx, req.Header); err != nil {
			return nil, err
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("forge: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return resp.Header, ErrNotFound
	}
	if resp.StatusCode >= 300 {
		return resp.Header, &APIError{Status: resp.StatusCode, Message: errorMessage(raw)}
	}
	switch o := out.(type) {
	case nil:
		return resp.Header, nil
	case *string:
		*o = string(raw)
		return resp.Header, nil
	default:
		if len(raw) == 0 {
			return resp.Header, nil
		}
		return resp.Header, json.Unmarshal(raw, out)
	}
}

// errorMessage reads "message" (or GitLab's "error") when it is a string, else keeps the raw body.
func errorMessage(raw []byte) string {
	msg := strings.TrimSpace(string(raw))
	var e map[string]json.RawMessage
	if json.Unmarshal(raw, &e) == nil {
		for _, k := range []string{"message", "error"} {
			var s string
			if json.Unmarshal(e[k], &s) == nil && s != "" {
				msg = s
				break
			}
		}
	}
	if len(msg) > 500 {
		msg = msg[:500]
	}
	return msg
}

// combine folds individual check states into one: any failure fails, any pending is pending.
func combine(checks []Check) string {
	if len(checks) == 0 {
		return "none"
	}
	state := "success"
	for _, c := range checks {
		switch c.State {
		case "failure", "error":
			return "failure"
		case "pending":
			state = "pending"
		}
	}
	return state
}
