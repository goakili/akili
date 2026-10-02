// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package miabi is a client for the Miabi PaaS REST API (github.com/miabi-io/miabi), used only on the
// control plane: the API key never reaches an agent.
package miabi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/goakili/akili/server/internal/crypto"
)

// Client talks to one Miabi workspace.
type Client struct {
	base      string
	workspace string
	key       string
	http      *http.Client
	// Poll is how often a deploy/rollback is checked while waiting for it.
	Poll time.Duration
}

// New returns a client. workspace is a Miabi workspace id, uid or handle.
func New(baseURL, workspace, apiKey string) *Client {
	return &Client{base: strings.TrimRight(baseURL, "/"), workspace: workspace, key: apiKey,
		http: &http.Client{Timeout: 60 * time.Second}, Poll: 3 * time.Second}
}

// TrustCA makes the client trust an extra CA (PEM), for a Miabi with a self-signed or private-CA
// certificate. The system roots stay trusted.
func (c *Client) TrustCA(caPEM string) error {
	cfg, err := crypto.TLSConfigWithCA(caPEM)
	if err != nil {
		return err
	}
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.TLSClientConfig = cfg
	c.http = &http.Client{Timeout: c.http.Timeout, Transport: t}
	return nil
}

// In returns a client for another workspace (id, uid or handle) with the same key.
func (c *Client) In(workspace string) *Client {
	cp := *c
	cp.workspace = workspace
	return &cp
}

// Workspace is a Miabi workspace the key's user belongs to.
type Workspace struct {
	ID          int64  `json:"id"`
	UID         string `json:"uid"`
	Name        string `json:"name"` // the handle
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
}

// Workspaces lists the workspaces of the key's user. Miabi refuses this to a key bound to one
// workspace (every service-account key); use Workspace for that one.
func (c *Client) Workspaces(ctx context.Context) ([]Workspace, error) {
	var ws []Workspace
	return ws, c.do(ctx, http.MethodGet, "/api/v1/workspaces", nil, &ws)
}

// Workspace fetches one workspace by handle, uid or id.
func (c *Client) Workspace(ctx context.Context, ref string) (*Workspace, error) {
	var w Workspace
	return &w, c.do(ctx, http.MethodGet, "/api/v1/workspaces/"+url.PathEscape(ref), nil, &w)
}

// KeyBinding describes the API key: the workspace it is bound to (0 when account-wide) and its scopes.
type KeyBinding struct {
	User        string
	WorkspaceID int64
	Scopes      []string
}

// Binding reads the key's binding from /me.
func (c *Client) Binding(ctx context.Context) (*KeyBinding, error) {
	var me struct {
		Email string `json:"email"`
		Name  string `json:"name"`
		Auth  *struct {
			WorkspaceID *int64   `json:"workspace_id"`
			Scopes      []string `json:"scopes"`
		} `json:"auth"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/me", nil, &me); err != nil {
		return nil, err
	}
	b := &KeyBinding{User: me.Email}
	if b.User == "" {
		b.User = me.Name
	}
	if me.Auth != nil {
		b.Scopes = me.Auth.Scopes
		if me.Auth.WorkspaceID != nil {
			b.WorkspaceID = *me.Auth.WorkspaceID
		}
	}
	return b, nil
}

// App is a Miabi application (the fields Akili uses).
type App struct {
	ID               int64  `json:"id"`
	UID              string `json:"uid"`
	Name             string `json:"name"`
	DisplayName      string `json:"display_name"`
	Status           string `json:"status"`
	SourceType       string `json:"source_type"`
	Image            string `json:"image"`
	Tag              string `json:"tag"`
	GitRepo          string `json:"git_repo"`
	GitRef           string `json:"git_ref"`
	Replicas         int    `json:"replicas"`
	CurrentReleaseID *int64 `json:"current_release_id"`
	RedeployRequired bool   `json:"redeploy_required"`
	HealthcheckType  string `json:"healthcheck_type"`
	HealthcheckPath  string `json:"healthcheck_http_path"`
}

// LiveStatus is read live from the container runtime.
type LiveStatus struct {
	Status         string `json:"status"`
	ContainerState string `json:"container_state"`
	Health         string `json:"health"`
	Running        bool   `json:"running"`
	RestartCount   int    `json:"restart_count"`
	ExitCode       int    `json:"exit_code"`
	UptimeSeconds  int64  `json:"uptime_seconds"`
	StoredStatus   string `json:"stored_status"`
}

// Deployment is one deploy attempt.
type Deployment struct {
	ID              int64      `json:"id"`
	Number          int        `json:"number"`
	Status          string     `json:"status"` // pending | building | deploying | canary | succeeded | failed
	Image           string     `json:"image"`
	Trigger         string     `json:"trigger"`
	Commit          string     `json:"commit"`
	Error           string     `json:"error"`
	TriggeredByName string     `json:"triggered_by_name"`
	CreatedAt       time.Time  `json:"created_at"`
	FinishedAt      *time.Time `json:"finished_at"`
	Current         bool       `json:"current"`
}

// Terminal reports whether a deployment has finished.
func (d Deployment) Terminal() bool { return d.Status == "succeeded" || d.Status == "failed" }

// Release is a deployable version (a rollback target).
type Release struct {
	ID           int64     `json:"id"`
	DeploymentID int64     `json:"deployment_id"`
	Version      int64     `json:"version"` // per-app release number
	Image        string    `json:"image"`
	Active       bool      `json:"active"`
	Pinned       bool      `json:"pinned"`
	Commit       string    `json:"commit"`
	CreatedAt    time.Time `json:"created_at"`
}

// Route is an ingress route (the app's URLs).
type Route struct {
	Hosts   []string `json:"hosts"`
	Path    string   `json:"path"`
	TLSMode string   `json:"tls_mode"`
}

// Error is a Miabi API error.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("miabi: %d %s: %s", e.Status, e.Code, e.Message) }

// ErrNotFound is returned for unknown apps or releases.
var ErrNotFound = errors.New("not found")

func (c *Client) ws() string { return "/api/v1/workspaces/" + url.PathEscape(c.workspace) }

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("miabi: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		if resp.StatusCode == http.StatusNotFound {
			return ErrNotFound
		}
		msg := e.Error.Message
		if msg == "" {
			msg = strings.TrimSpace(string(raw))
			if len(msg) > 300 {
				msg = msg[:300]
			}
		}
		return &Error{Status: resp.StatusCode, Code: e.Error.Code, Message: msg}
	}
	if out == nil {
		return nil
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("miabi: bad response: %w", err)
	}
	return json.Unmarshal(env.Data, out)
}

// Me checks the key and returns the user it acts as.
func (c *Client) Me(ctx context.Context) (string, error) {
	var me struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/me", nil, &me); err != nil {
		return "", err
	}
	if me.Email != "" {
		return me.Email, nil
	}
	return me.Name, nil
}

// Apps lists the workspace's applications.
func (c *Client) Apps(ctx context.Context) ([]App, error) {
	var apps []App
	return apps, c.do(ctx, http.MethodGet, c.ws()+"/apps", nil, &apps)
}

// ResolveName finds an app by its name (slug) only. Agent tool calls use this: policies match app
// names, so an id or uid must not reach an app the name rules would refuse.
func (c *Client) ResolveName(ctx context.Context, name string) (*App, error) {
	apps, err := c.Apps(ctx)
	if err != nil {
		return nil, err
	}
	for i := range apps {
		if strings.EqualFold(apps[i].Name, name) {
			return &apps[i], nil
		}
	}
	return nil, fmt.Errorf("%w: no Miabi app named %q in workspace %s", ErrNotFound, name, c.workspace)
}

// Resolve finds an app by name, display name, numeric id or uid (Miabi paths take only ids).
func (c *Client) Resolve(ctx context.Context, ref string) (*App, error) {
	apps, err := c.Apps(ctx)
	if err != nil {
		return nil, err
	}
	for i := range apps {
		a := &apps[i]
		if strings.EqualFold(a.Name, ref) || a.UID == ref || strconv.FormatInt(a.ID, 10) == ref || strings.EqualFold(a.DisplayName, ref) {
			return a, nil
		}
	}
	return nil, fmt.Errorf("%w: no Miabi app %q in workspace %s", ErrNotFound, ref, c.workspace)
}

func (c *Client) app(id int64) string { return c.ws() + "/apps/" + strconv.FormatInt(id, 10) }

// Status returns an app's live status.
func (c *Client) Status(ctx context.Context, appID int64) (*LiveStatus, error) {
	var st LiveStatus
	return &st, c.do(ctx, http.MethodGet, c.app(appID)+"/status", nil, &st)
}

// Deployments lists an app's recent deployments, newest first.
func (c *Client) Deployments(ctx context.Context, appID int64) ([]Deployment, error) {
	var ds []Deployment
	return ds, c.do(ctx, http.MethodGet, c.app(appID)+"/deployments", nil, &ds)
}

// Releases lists an app's releases.
func (c *Client) Releases(ctx context.Context, appID int64) ([]Release, error) {
	var rs []Release
	return rs, c.do(ctx, http.MethodGet, c.app(appID)+"/releases", nil, &rs)
}

// Routes lists the ingress routes to an app.
func (c *Client) Routes(ctx context.Context, appID int64) ([]Route, error) {
	var rs []Route
	return rs, c.do(ctx, http.MethodGet, c.ws()+"/routes?application_id="+strconv.FormatInt(appID, 10), nil, &rs)
}

// Deploy starts a deployment (optionally of a tag). Apps whose repository owns a pipeline start a
// pipeline run instead; that is reported as an error because its outcome is not a deployment.
func (c *Client) Deploy(ctx context.Context, appID int64, tag string) (*Deployment, error) {
	body := map[string]any{}
	if tag != "" {
		body["tag"] = tag
	}
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodPost, c.app(appID)+"/deploy", body, &raw); err != nil {
		return nil, err
	}
	var kind struct {
		Kind string `json:"kind"`
	}
	if json.Unmarshal(raw, &kind) == nil && kind.Kind == "pipeline_run" {
		return nil, errors.New("this app deploys through a Miabi pipeline; trigger the pipeline (or push to its branch) instead")
	}
	var d Deployment
	return &d, json.Unmarshal(raw, &d)
}

// Rollback redeploys a release's image.
func (c *Client) Rollback(ctx context.Context, appID, releaseID int64) (*Deployment, error) {
	var d Deployment
	return &d, c.do(ctx, http.MethodPost, c.app(appID)+"/rollback", map[string]any{"release_id": releaseID}, &d)
}

// Restart restarts an app's containers.
func (c *Client) Restart(ctx context.Context, appID int64) error {
	return c.do(ctx, http.MethodPost, c.app(appID)+"/restart", map[string]any{}, nil)
}

// DeployLogs returns the history of a deployment's log.
func (c *Client) DeployLogs(ctx context.Context, appID, deploymentID int64) ([]string, string, error) {
	var out struct {
		Status    string   `json:"status"`
		Lines     []string `json:"lines"`
		Truncated bool     `json:"truncated"`
	}
	err := c.do(ctx, http.MethodGet, fmt.Sprintf("%s/deployments/%d/logs/history", c.app(appID), deploymentID), nil, &out)
	return out.Lines, out.Status, err
}

// Logs reads a snapshot of an app's container logs (the SSE stream with follow=false).
func (c *Client) Logs(ctx context.Context, appID int64, lines int) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s%s/logs/stream?tail=%d&follow=false", c.base, c.app(appID), lines), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("miabi: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusConflict {
		return "", errors.New("the app has no running container")
	}
	if resp.StatusCode >= 300 {
		return "", &Error{Status: resp.StatusCode, Message: "logs unavailable"}
	}
	var b strings.Builder
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 4<<20))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line, ok := strings.CutPrefix(sc.Text(), "data:")
		if !ok {
			continue
		}
		var ev struct {
			Stream string `json:"stream"`
			Text   string `json:"text"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(line)), &ev) == nil && ev.Text != "" {
			b.WriteString(strings.TrimRight(ev.Text, "\n"))
			b.WriteByte('\n')
		}
	}
	return b.String(), nil
}

// Wait polls until a deployment finishes or the timeout passes.
func (c *Client) Wait(ctx context.Context, appID, deploymentID int64, timeout time.Duration) (*Deployment, error) {
	deadline := time.Now().Add(timeout)
	for {
		ds, err := c.Deployments(ctx, appID)
		if err != nil {
			return nil, err
		}
		for i := range ds {
			if ds[i].ID == deploymentID && ds[i].Terminal() {
				return &ds[i], nil
			}
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("deployment %d did not finish within %s", deploymentID, timeout)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(c.Poll):
		}
	}
}

// Event is a workspace event (deploys, containers, databases, backups, drift).
type Event struct {
	ID            int64             `json:"id"`
	WorkspaceID   int64             `json:"workspace_id"`
	SubjectType   string            `json:"subject_type"` // app | database
	ApplicationID int64             `json:"application_id"`
	DatabaseID    int64             `json:"database_id"`
	Type          string            `json:"type"`
	Severity      string            `json:"severity"`
	Message       string            `json:"message"`
	Metadata      map[string]string `json:"metadata"`
	CreatedAt     time.Time         `json:"created_at"`
	AppName       string            `json:"app_name"` // history only
	DatabaseName  string            `json:"database_name"`
}

// RecentEvents returns the workspace's latest events, oldest first.
func (c *Client) RecentEvents(ctx context.Context, limit int) ([]Event, error) {
	var evs []Event
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("%s/events?page=1&size=%d&order=desc", c.ws(), limit), nil, &evs); err != nil {
		return nil, err
	}
	sort.Slice(evs, func(i, j int) bool { return evs[i].ID < evs[j].ID })
	return evs, nil
}

// StreamEvents follows the workspace's live event stream (SSE) until ctx ends or the stream breaks.
func (c *Client) StreamEvents(ctx context.Context, handle func(Event)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+c.ws()+"/events/stream", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Accept", "text/event-stream")
	// No client timeout: the stream is long-lived; ctx ends it.
	resp, err := (&http.Client{Transport: c.http.Transport}).Do(req)
	if err != nil {
		return fmt.Errorf("miabi: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return &Error{Status: resp.StatusCode, Message: "event stream unavailable"}
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line, ok := strings.CutPrefix(sc.Text(), "data:")
		if !ok {
			continue
		}
		var msg struct {
			Type string `json:"type"`
			Data Event  `json:"data"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(line)), &msg) == nil && msg.Type == "event" && msg.Data.ID != 0 {
			handle(msg.Data)
		}
	}
	if err := sc.Err(); err != nil && ctx.Err() == nil {
		return err
	}
	return errors.New("event stream closed")
}
