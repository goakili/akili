// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package miabitest is an in-memory Miabi API for tests: workspaces, apps, releases, deployments
// that finish after a delay (and emit events, like Miabi), per-tag health, container logs, and the
// workspace event history and live stream. It implements only what Akili calls.
package miabitest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Fake is the fake server state.
type Fake struct {
	Key         string
	DeployDelay time.Duration

	mu         sync.Mutex
	workspaces []*workspace
	nextID     int64
	nextEvent  int64
	unhealthy  map[string]bool // image tags whose containers report unhealthy
	boundWS    int64           // the key's bound workspace id (0: account-wide)
	subs       map[int64][]chan []byte
}

type workspace struct {
	ID       int64
	UID      string
	Name     string
	Apps     []*app
	Events   []map[string]any
	Backups  []map[string]any
	Restored int
	Alerts   []map[string]any
	CronRuns int
	Pipeline int // runs started
}

type app struct {
	ID          int64
	Name        string
	Image       string
	Releases    []*release
	Deployments []*deployment
	Restarts    int
	Replicas    int
	Env         []map[string]any
	Maintenance bool
	Canary      bool
}

type release struct {
	ID      int64     `json:"id"`
	Number  int64     `json:"version"` // Miabi numbers releases per app
	Version string    `json:"-"`       // the image tag
	Image   string    `json:"image"`
	Active  bool      `json:"active"`
	Created time.Time `json:"created_at"`
}

type deployment struct {
	ID       int64      `json:"id"`
	Number   int        `json:"number"`
	Status   string     `json:"status"`
	Image    string     `json:"image"`
	Trigger  string     `json:"trigger"`
	Error    string     `json:"error,omitempty"`
	Created  time.Time  `json:"created_at"`
	Finished *time.Time `json:"finished_at"`
	Current  bool       `json:"current"`
	tag      string
	target   *release
}

// New returns a fake with the workspaces "staging" (id 7) and "prod" (id 8), each with one app
// ("api", image example/api) whose active release is the last tag.
func New(key string, tags ...string) *Fake {
	f := &Fake{Key: key, DeployDelay: 300 * time.Millisecond, nextID: 100, unhealthy: map[string]bool{}, subs: map[int64][]chan []byte{}}
	for i, name := range []string{"staging", "prod"} {
		ws := &workspace{ID: int64(7 + i), UID: fmt.Sprintf("ws-uid-%d", 7+i), Name: name}
		a := &app{ID: int64(1 + 10*i), Name: "api", Image: "example/api", Replicas: 1,
			Env: []map[string]any{{"key": "LOG_LEVEL", "value": "info", "is_secret": false}, {"key": "DATABASE_PASSWORD", "value": "••••••••", "is_secret": true}}}
		ws.Alerts = []map[string]any{{"id": 900 + i, "category": "runtime", "severity": "warning", "state": "firing", "title": "High memory on api",
			"body": "memory above 90% for 10 minutes", "count": 3, "subject_type": "app", "subject_ref": "api", "last_seen": time.Now().UTC()}}
		for j, t := range tags {
			a.Releases = append(a.Releases, &release{ID: f.id(), Number: int64(j + 1), Version: t, Image: a.Image + ":" + t, Active: j == len(tags)-1,
				Created: time.Now().Add(time.Duration(j-len(tags)) * time.Hour)})
		}
		ws.Apps = []*app{a}
		f.workspaces = append(f.workspaces, ws)
	}
	return f
}

func (f *Fake) id() int64 { f.nextID++; return f.nextID }

// SetUnhealthy marks an image tag as unhealthy (or healthy again).
func (f *Fake) SetUnhealthy(tag string, bad bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unhealthy[tag] = bad
}

// ActiveTag returns the active release tag of an app in the first workspace (staging).
func (f *Fake) ActiveTag(name string) string { return f.ActiveTagIn("staging", name) }

// ActiveTagIn returns an app's active release tag in a workspace.
func (f *Fake) ActiveTagIn(ws, name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tick()
	if w := f.findWS(ws); w != nil {
		for _, a := range w.Apps {
			if a.Name == name {
				if r := a.active(); r != nil {
					return r.Version
				}
			}
		}
	}
	return ""
}

func (a *app) active() *release {
	for _, r := range a.Releases {
		if r.Active {
			return r
		}
	}
	return nil
}

// findWS resolves a workspace by id, uid or handle, as Miabi does.
func (f *Fake) findWS(ref string) *workspace {
	for _, w := range f.workspaces {
		if w.Name == ref || w.UID == ref || strconv.FormatInt(w.ID, 10) == ref {
			return w
		}
	}
	return nil
}

func ok(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data})
}

func fail(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": map[string]any{"status_code": code, "code": http.StatusText(code), "message": msg}})
}

// ServeHTTP implements the subset of the Miabi API that Akili uses, plus /_fake controls.
func (f *Fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/_fake/") {
		f.control(w, r)
		return
	}
	if strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") != f.Key {
		fail(w, http.StatusUnauthorized, "invalid API key")
		return
	}
	switch r.URL.Path {
	case "/api/v1/me":
		f.mu.Lock()
		auth := map[string]any{"method": "api_key", "scopes": []string{"read", "write", "deploy"}}
		if f.boundWS != 0 {
			auth["workspace_id"] = f.boundWS
		}
		f.mu.Unlock()
		ok(w, 200, map[string]any{"email": "akili@miabi.test", "name": "Akili", "auth": auth})
		return
	case "/api/v1/workspaces":
		f.mu.Lock()
		if f.boundWS != 0 {
			f.mu.Unlock()
			fail(w, 403, "this API key is scoped to a workspace and cannot be used on account-level endpoints")
			return
		}
		var out []map[string]any
		for _, ws := range f.workspaces {
			out = append(out, map[string]any{"id": ws.ID, "uid": ws.UID, "name": ws.Name, "display_name": strings.ToUpper(ws.Name[:1]) + ws.Name[1:], "role": "developer"})
		}
		f.mu.Unlock()
		ok(w, 200, out)
		return
	}
	p := strings.Split(strings.Trim(r.URL.Path, "/"), "/") // api v1 workspaces {ws} ...
	if len(p) < 4 || p[0] != "api" || p[1] != "v1" || p[2] != "workspaces" {
		fail(w, 404, "route not found")
		return
	}
	f.mu.Lock()
	f.tick()
	ws := f.findWS(p[3])
	if ws == nil {
		f.mu.Unlock()
		fail(w, 404, "workspace not found")
		return
	}
	if f.boundWS != 0 && f.boundWS != ws.ID {
		f.mu.Unlock()
		fail(w, 403, "this API key is scoped to a different workspace")
		return
	}
	if len(p) == 6 && p[4] == "events" && p[5] == "stream" {
		ch := make(chan []byte, 64)
		f.subs[ws.ID] = append(f.subs[ws.ID], ch)
		f.mu.Unlock()
		f.stream(w, r, ws.ID, ch)
		return
	}
	defer f.mu.Unlock()
	switch {
	case len(p) == 4:
		ok(w, 200, map[string]any{"id": ws.ID, "uid": ws.UID, "name": ws.Name, "role": "developer"})
	case len(p) == 5 && p[4] == "events":
		out := make([]map[string]any, 0, len(ws.Events))
		for i := len(ws.Events) - 1; i >= 0 && len(out) < 100; i-- {
			out = append(out, ws.Events[i])
		}
		ok(w, 200, out)
	case len(p) == 5 && p[4] == "apps" && r.Method == http.MethodGet:
		var out []map[string]any
		for _, a := range ws.Apps {
			out = append(out, f.appJSON(a))
		}
		ok(w, 200, out)
	case len(p) == 5 && p[4] == "routes":
		a := ws.Apps[0]
		ok(w, 200, []map[string]any{{"id": 700 + ws.ID, "name": "api-web", "hosts": []string{"api." + ws.Name + ".example.test"}, "path": "/", "tls_mode": "auto",
			"application_id": a.ID, "maintenance": map[string]any{"enabled": a.Maintenance}}})
	case len(p) == 7 && p[4] == "routes" && p[6] == "maintenance" && r.Method == http.MethodPatch:
		var body struct {
			Enabled bool `json:"enabled"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		ws.Apps[0].Maintenance = body.Enabled
		ok(w, 200, map[string]any{"id": p[5], "maintenance": map[string]any{"enabled": body.Enabled}})
	case len(p) == 5 && p[4] == "overview":
		var apps []map[string]any
		running := 0
		for _, a := range ws.Apps {
			health := "healthy"
			if act := a.active(); act != nil && f.unhealthy[act.Version] {
				health = "unhealthy"
			}
			running++
			apps = append(apps, map[string]any{"id": a.ID, "name": a.Name, "status": "running", "health": health})
		}
		recent := ws.Events
		if len(recent) > 10 {
			recent = recent[len(recent)-10:]
		}
		ok(w, 200, map[string]any{"apps": apps, "total_apps": len(ws.Apps), "running": running, "failed": 0, "databases": 1, "stacks": 1, "recent_events": recent})
	case len(p) == 5 && p[4] == "usage":
		q := func(used int64, limit int) map[string]any { return map[string]any{"used": used, "limit": limit} }
		ok(w, 200, map[string]any{"plan_name": "Team", "apps": q(int64(len(ws.Apps)), 20), "database_instances": q(1, 5), "cpu_cores": q(2, -1), "memory_mb": q(1024, 8192)})
	case len(p) == 5 && p[4] == "alerts":
		out := []map[string]any{}
		for _, a := range ws.Alerts {
			if r.URL.Query().Get("active") == "true" && a["state"] != "firing" && a["state"] != "acknowledged" {
				continue
			}
			out = append(out, a)
		}
		ok(w, 200, out)
	case len(p) == 7 && p[4] == "alerts" && r.Method == http.MethodPost:
		for _, a := range ws.Alerts {
			if fmt.Sprint(a["id"]) == p[5] {
				a["state"] = map[string]string{"ack": "acknowledged", "resolve": "resolved"}[p[6]]
				ok(w, 200, a)
				return
			}
		}
		fail(w, 404, "alert not found")
	case len(p) == 5 && p[4] == "databases":
		ok(w, 200, []map[string]any{{"id": 501, "name": "pg-main", "engine": "postgres", "version": "17", "status": "running", "size_bytes": 50 << 20}})
	case len(p) >= 6 && p[4] == "databases":
		f.database(w, r, ws, p[5:])
	case len(p) == 5 && p[4] == "analytics":
		fail(w, 404, "route not found")
	case len(p) == 6 && p[4] == "analytics" && p[5] == "summary":
		id, _ := strconv.ParseInt(r.URL.Query().Get("app"), 10, 64)
		if r.URL.Query().Get("app") != "" && id == 0 {
			fail(w, 400, "invalid app id")
			return
		}
		rate, p95 := 0.004, 120.0
		for _, a := range ws.Apps {
			if a.ID == id {
				if act := a.active(); act != nil && f.unhealthy[act.Version] {
					rate, p95 = 0.35, 2400
				}
			}
		}
		ok(w, 200, map[string]any{"totals": map[string]any{"requests": 1200, "error_rate": rate, "p95_latency_ms": p95, "p99_latency_ms": p95 * 1.5, "avg_latency_ms": p95 / 2},
			"status": map[string]any{"s5xx": int(rate * 1200)}})
	case len(p) == 5 && p[4] == "stacks":
		ok(w, 200, []map[string]any{{"id": 41, "name": "web", "app_count": len(ws.Apps), "status": map[string]any{"total": len(ws.Apps), "running": len(ws.Apps)}}})
	case len(p) == 7 && p[4] == "stacks" && p[6] == "restart":
		var out []map[string]any
		for _, a := range ws.Apps {
			a.Restarts++
			out = append(out, map[string]any{"app_id": a.ID, "app_name": a.Name, "status": "ok"})
		}
		ok(w, 200, out)
	case len(p) == 5 && p[4] == "cronjobs":
		ok(w, 200, []map[string]any{{"id": 61, "name": "nightly-report", "app_name": "api", "schedule": "0 3 * * *", "enabled": true}})
	case len(p) == 7 && p[4] == "cronjobs" && p[6] == "run":
		ws.CronRuns++
		ok(w, 201, map[string]any{"id": 6100 + ws.CronRuns, "status": "pending"})
	case len(p) == 6 && p[4] == "jobs":
		id, _ := strconv.ParseInt(p[5], 10, 64)
		ok(w, 200, map[string]any{"id": id, "status": "succeeded", "exit_code": 0})
	case len(p) == 5 && p[4] == "pipelines":
		ok(w, 200, []map[string]any{{"id": 71, "name": "build-api", "branch": "main", "enabled": true}})
	case len(p) == 7 && p[4] == "pipelines" && p[6] == "trigger":
		ws.Pipeline++
		ok(w, 201, map[string]any{"id": 7100 + ws.Pipeline, "number": ws.Pipeline, "status": "running", "branch": "main", "commit": "abc1234"})
	case len(p) == 6 && p[4] == "pipeline-runs":
		id, _ := strconv.ParseInt(p[5], 10, 64)
		ok(w, 200, map[string]any{"id": id, "number": ws.Pipeline, "status": "succeeded", "branch": "main", "commit": "abc1234"})
	case len(p) >= 6 && p[4] == "apps":
		id, _ := strconv.ParseInt(p[5], 10, 64)
		var a *app
		for _, x := range ws.Apps {
			if x.ID == id {
				a = x
			}
		}
		if a == nil {
			fail(w, 404, "application not found")
			return
		}
		f.appRoute(w, r, ws, a, p[6:])
	default:
		fail(w, 404, "route not found")
	}
}

func (f *Fake) stream(w http.ResponseWriter, r *http.Request, wsID int64, ch chan []byte) {
	defer func() {
		f.mu.Lock()
		list := f.subs[wsID]
		for i, c := range list {
			if c == ch {
				f.subs[wsID] = append(list[:i], list[i+1:]...)
				break
			}
		}
		f.mu.Unlock()
	}()
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	fl, _ := w.(http.Flusher)
	if fl != nil {
		fl.Flush()
	}
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case b := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", b)
			if fl != nil {
				fl.Flush()
			}
		case <-tick.C:
			// Deployments finish on a timer even when nobody polls.
			f.mu.Lock()
			f.tick()
			f.mu.Unlock()
		}
	}
}

// emit records a workspace event and pushes it to live streams. The caller holds f.mu.
func (f *Fake) emit(ws *workspace, typ string, a *app, severity, msg string, meta map[string]string) map[string]any {
	return f.emitDB(ws, typ, a, "", severity, msg, meta)
}

func (f *Fake) emitDB(ws *workspace, typ string, a *app, db, severity, msg string, meta map[string]string) map[string]any {
	f.nextEvent++
	ev := map[string]any{"id": f.nextEvent, "workspace_id": ws.ID, "subject_type": "app", "type": typ, "severity": severity,
		"message": msg, "metadata": meta, "created_at": time.Now().UTC()}
	if a != nil {
		ev["application_id"], ev["app_name"] = a.ID, a.Name
	}
	if db != "" {
		ev["subject_type"], ev["database_id"], ev["database_name"] = "database", 501, db
	}
	ws.Events = append(ws.Events, ev)
	live := map[string]any{}
	for k, v := range ev {
		if k != "app_name" && k != "database_name" { // the live stream carries ids only, like Miabi
			live[k] = v
		}
	}
	b, _ := json.Marshal(map[string]any{"type": "event", "data": live})
	for _, ch := range f.subs[ws.ID] {
		select {
		case ch <- b:
		default:
		}
	}
	return ev
}

func (f *Fake) appJSON(a *app) map[string]any {
	tag := ""
	var cur *int64
	if r := a.active(); r != nil {
		tag, cur = r.Version, &r.ID
	}
	return map[string]any{"id": a.ID, "uid": fmt.Sprintf("uid-%d", a.ID), "name": a.Name, "display_name": strings.ToUpper(a.Name[:1]) + a.Name[1:],
		"status": "running", "source_type": "image", "image": a.Image, "tag": tag, "replicas": 1, "current_release_id": cur,
		"healthcheck_type": "http", "healthcheck_http_path": "/healthz"}
}

func (f *Fake) appRoute(w http.ResponseWriter, r *http.Request, ws *workspace, a *app, rest []string) {
	switch {
	case len(rest) == 0:
		ok(w, 200, f.appJSON(a))
	case rest[0] == "status":
		health, status := "healthy", "running"
		if act := a.active(); act == nil {
			health, status = "none", "no_container"
		} else if f.unhealthy[act.Version] {
			health, status = "unhealthy", "unhealthy"
		}
		ok(w, 200, map[string]any{"status": status, "container_state": "running", "health": health, "running": true,
			"restart_count": a.Restarts, "uptime_seconds": 42, "stored_status": "running"})
	case rest[0] == "deployments" && len(rest) == 1:
		out := make([]*deployment, 0, len(a.Deployments))
		for i := len(a.Deployments) - 1; i >= 0; i-- {
			out = append(out, a.Deployments[i])
		}
		ok(w, 200, out)
	case rest[0] == "deployments" && len(rest) == 4 && rest[2] == "logs" && rest[3] == "history":
		ok(w, 200, map[string]any{"status": "succeeded", "lines": []string{"pulling image", "starting container", "health gate passed"}, "truncated": false})
	case rest[0] == "releases":
		ok(w, 200, a.Releases)
	case rest[0] == "deploy" && r.Method == http.MethodPost:
		var body struct {
			Tag string `json:"tag"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		tag := body.Tag
		if tag == "" {
			if act := a.active(); act != nil {
				tag = act.Version
			}
		}
		ok(w, 201, f.startDeploy(ws, a, tag, "manual", nil))
	case rest[0] == "rollback" && r.Method == http.MethodPost:
		var body struct {
			ReleaseID int64 `json:"release_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		for _, rel := range a.Releases {
			if rel.ID == body.ReleaseID {
				ok(w, 201, f.startDeploy(ws, a, rel.Version, "rollback", rel))
				return
			}
		}
		fail(w, 400, "release not found")
	case rest[0] == "env" && r.Method == http.MethodGet:
		ok(w, 200, a.Env)
	case rest[0] == "env" && r.Method == http.MethodPut:
		var body struct {
			Key      string `json:"key"`
			Value    string `json:"value"`
			IsSecret bool   `json:"is_secret"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		for _, e := range a.Env {
			if e["key"] == body.Key {
				e["value"] = body.Value
				ok(w, 200, map[string]string{"message": "environment variable set — redeploy required"})
				return
			}
		}
		a.Env = append(a.Env, map[string]any{"key": body.Key, "value": body.Value, "is_secret": body.IsSecret})
		ok(w, 200, map[string]string{"message": "environment variable set — redeploy required"})
	case rest[0] == "scale" && r.Method == http.MethodPost:
		var body struct {
			Replicas int `json:"replicas"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		a.Replicas = body.Replicas
		ok(w, 200, map[string]string{"message": "application scaled"})
	case rest[0] == "canary" && len(rest) == 2 && rest[1] == "promote":
		if !a.Canary {
			fail(w, 409, "no canary deployment in progress")
			return
		}
		a.Canary = false
		ok(w, 201, f.startDeploy(ws, a, "canary", "promote", nil))
	case rest[0] == "canary" && len(rest) == 1 && r.Method == http.MethodDelete:
		if !a.Canary {
			fail(w, 409, "no canary deployment in progress")
			return
		}
		a.Canary = false
		ok(w, 200, map[string]string{"message": "canary aborted"})
	case rest[0] == "restart" && r.Method == http.MethodPost:
		a.Restarts++
		ok(w, 200, map[string]string{"message": "application restart requested"})
	case rest[0] == "logs":
		w.Header().Set("Content-Type", "text/event-stream")
		tag := ""
		if act := a.active(); act != nil {
			tag = act.Version
		}
		for _, line := range []string{"listening on :8080", "GET /healthz 200"} {
			if f.unhealthy[tag] && strings.Contains(line, "healthz") {
				line = "GET /healthz 500 database connection refused"
			}
			b, _ := json.Marshal(map[string]string{"stream": "stdout", "text": line})
			fmt.Fprintf(w, "data: %s\n\n", b)
		}
	default:
		fail(w, 404, "route not found")
	}
}

// startDeploy records a deployment that completes after DeployDelay (see tick).
func (f *Fake) startDeploy(ws *workspace, a *app, tag, trigger string, target *release) *deployment {
	d := &deployment{ID: f.id(), Number: len(a.Deployments) + 1, Status: "deploying", Image: a.Image + ":" + tag, Trigger: trigger,
		Created: time.Now(), tag: tag, target: target}
	a.Deployments = append(a.Deployments, d)
	f.emit(ws, "deploy.started", a, "info", "deployment started", map[string]string{"deployment_id": strconv.FormatInt(d.ID, 10)})
	return d
}

// tick finishes deployments whose delay has passed and emits their events. The caller holds f.mu.
func (f *Fake) tick() {
	for _, ws := range f.workspaces {
		for _, a := range ws.Apps {
			for _, d := range a.Deployments {
				if d.Status != "deploying" || time.Since(d.Created) < f.DeployDelay {
					continue
				}
				now := time.Now()
				d.Status, d.Finished = "succeeded", &now
				for _, x := range a.Deployments {
					x.Current = false
				}
				d.Current = true
				rel := d.target
				if rel == nil {
					rel = &release{ID: f.id(), Number: int64(len(a.Releases) + 1), Version: d.tag, Image: d.Image, Created: now}
					a.Releases = append(a.Releases, rel)
				}
				for _, x := range a.Releases {
					x.Active = x == rel
				}
				typ := "deploy.succeeded"
				if d.Trigger == "rollback" {
					typ = "release.activated" // not a watched event: a rollback does not ask for another verification
				}
				f.emit(ws, typ, a, "info", fmt.Sprintf("deployment #%d succeeded", d.Number), map[string]string{"deployment_id": strconv.FormatInt(d.ID, 10)})
			}
		}
	}
}

// database serves /databases/{id}/... for the one instance (pg-main, id 501) with one logical
// database ("app", id 601).
func (f *Fake) database(w http.ResponseWriter, r *http.Request, ws *workspace, rest []string) {
	if rest[0] != "501" {
		fail(w, 404, "database not found")
		return
	}
	switch {
	case len(rest) == 2 && rest[1] == "status":
		ok(w, 200, map[string]any{"status": "running", "health": "healthy", "running": true})
	case len(rest) == 2 && rest[1] == "databases":
		ok(w, 200, []map[string]any{{"id": 601, "name": "app", "status": "ready"}})
	case len(rest) == 4 && rest[1] == "databases" && rest[2] == "601" && rest[3] == "backups" && r.Method == http.MethodGet:
		out := make([]map[string]any, 0, len(ws.Backups))
		for i := len(ws.Backups) - 1; i >= 0; i-- {
			out = append(out, ws.Backups[i])
		}
		ok(w, 200, out)
	case len(rest) == 4 && rest[1] == "databases" && rest[2] == "601" && rest[3] == "backups" && r.Method == http.MethodPost:
		b := map[string]any{"id": 8000 + len(ws.Backups) + 1, "number": len(ws.Backups) + 1, "status": "completed", "trigger": "manual",
			"destination": "local", "size_bytes": 4 << 20, "created_at": time.Now().UTC()}
		ws.Backups = append(ws.Backups, b)
		ok(w, 200, b)
	case len(rest) == 6 && rest[3] == "backups" && rest[5] == "restore" && r.Method == http.MethodPost:
		var body struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Method == "force" {
			fail(w, 400, "the fake refuses force restores")
			return
		}
		ws.Restored++
		ok(w, 200, map[string]string{"message": "database restored"})
	default:
		fail(w, 404, "route not found")
	}
}

func (f *Fake) control(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/_fake/canary":
		var body struct {
			Workspace string `json:"workspace"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		if ws := f.findWS(body.Workspace); ws != nil {
			ws.Apps[0].Canary = true
		}
		f.mu.Unlock()
		ok(w, 200, body)
	case "/_fake/unhealthy":
		var body struct {
			Tag       string `json:"tag"`
			Unhealthy bool   `json:"unhealthy"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			fail(w, 400, "bad body")
			return
		}
		f.SetUnhealthy(body.Tag, body.Unhealthy)
		ok(w, 200, map[string]any{"tag": body.Tag, "unhealthy": body.Unhealthy})
	case "/_fake/bind":
		// Bind the key to one workspace (by id; 0 makes it account-wide again).
		var body struct {
			WorkspaceID int64 `json:"workspace_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.boundWS = body.WorkspaceID
		f.mu.Unlock()
		ok(w, 200, body)
	case "/_fake/event":
		var body struct {
			Workspace string            `json:"workspace"`
			Type      string            `json:"type"`
			App       string            `json:"app"`
			Database  string            `json:"database"`
			Message   string            `json:"message"`
			Metadata  map[string]string `json:"metadata"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		defer f.mu.Unlock()
		ws := f.findWS(body.Workspace)
		if ws == nil {
			fail(w, 404, "workspace not found")
			return
		}
		var a *app
		for _, x := range ws.Apps {
			if x.Name == body.App {
				a = x
			}
		}
		ok(w, 200, f.emitDB(ws, body.Type, a, body.Database, "error", body.Message, body.Metadata))
	case "/_fake/state":
		f.mu.Lock()
		f.tick()
		out := map[string]any{}
		for _, ws := range f.workspaces {
			for _, a := range ws.Apps {
				tag := ""
				if act := a.active(); act != nil {
					tag = act.Version
				}
				out[ws.Name+"/"+a.Name] = map[string]any{"active": tag, "deployments": len(a.Deployments), "restarts": a.Restarts,
					"replicas": a.Replicas, "maintenance": a.Maintenance, "canary": a.Canary, "env": a.Env}
			}
			out[ws.Name] = map[string]any{"backups": len(ws.Backups), "restored": ws.Restored, "cron_runs": ws.CronRuns, "pipeline_runs": ws.Pipeline, "alerts": ws.Alerts}
		}
		f.mu.Unlock()
		ok(w, 200, out)
	default:
		fail(w, 404, "unknown control")
	}
}
