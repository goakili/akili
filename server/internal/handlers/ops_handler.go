// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/alerts"
	"github.com/goakili/akili/server/internal/audit"
	"github.com/goakili/akili/server/internal/auth"
	"github.com/goakili/akili/server/internal/bus"
	"github.com/goakili/akili/server/internal/crypto"
	"github.com/goakili/akili/server/internal/middlewares"
	"github.com/goakili/akili/server/internal/models"
	"github.com/goakili/akili/server/internal/storage/pagination"
	"github.com/goakili/akili/server/internal/tasks"
	"github.com/gorilla/websocket"
	"github.com/jkaninda/logger"
	"github.com/jkaninda/okapi"
)

// ---- changes ----

// ListChanges lists change plans, newest first.
func (h *Handlers) ListChanges(c *okapi.Context) error {
	q := h.DB.Where("organization_id = ?", middlewares.OrgID(c))
	for _, f := range []string{"status", "agent_id", "session_id", "task_id"} {
		if v := c.Query(f); v != "" {
			q = q.Where(f+" = ?", v)
		}
	}
	p := pageParams(c)
	out, total, err := pagination.Find[models.Change](q, p, "created_at DESC, id DESC")
	if err != nil {
		return c.AbortInternalServerError("list failed", err)
	}
	return paged(c, out, total, p)
}

// GetChange returns one change plan with its call outcomes.
func (h *Handlers) GetChange(c *okapi.Context) error {
	var ch models.Change
	if err := h.DB.First(&ch, "id = ? AND organization_id = ?", c.Param("id"), middlewares.OrgID(c)).Error; err != nil {
		return c.AbortNotFound("change not found")
	}
	return ok(c, ch)
}

// ---- alert routes ----

// AlertRouteRequest creates or updates an alert route.
type AlertRouteRequest struct {
	Body struct {
		Name         string            `json:"name" required:"true"`
		Enabled      *bool             `json:"enabled"`
		AgentID      *string           `json:"agent_id"`
		Selector     []string          `json:"selector"`
		HostLabel    string            `json:"host_label" description:"alert label naming the host (default instance)"`
		Match        map[string]string `json:"match" description:"labels an alert must carry"`
		Autonomy     *proto.Autonomy   `json:"autonomy" description:"default L2; fixes still need an approved change plan"`
		Instructions string            `json:"instructions"`
	} `json:"body"`
}

// AlertRouteCreated returns the route and its webhook URL (with the secret token) once.
type AlertRouteCreated struct {
	Route      *models.AlertRoute `json:"route"`
	WebhookURL string             `json:"webhook_url"`
}

func (h *Handlers) applyRoute(r *models.AlertRoute, req *AlertRouteRequest) error {
	b := req.Body
	if strings.TrimSpace(b.Name) == "" {
		return errors.New("name is required")
	}
	r.Name, r.AgentID, r.Selector, r.HostLabel, r.Match, r.Instructions = strings.TrimSpace(b.Name), emptyNil(b.AgentID), nonNilList(b.Selector), strings.TrimSpace(b.HostLabel), b.Match, b.Instructions
	if r.Match == nil {
		r.Match = map[string]string{}
	}
	if b.Enabled != nil {
		r.Enabled = *b.Enabled
	}
	if b.Autonomy != nil {
		if !b.Autonomy.Valid() {
			return errors.New("autonomy must be 0-3")
		}
		r.Autonomy = *b.Autonomy
	}
	return nil
}

// ListAlertRoutes lists alert routes.
func (h *Handlers) ListAlertRoutes(c *okapi.Context) error {
	var out []models.AlertRoute
	h.DB.Where("organization_id = ?", middlewares.OrgID(c)).Order("name").Find(&out)
	return ok(c, out)
}

// CreateAlertRoute adds a route and returns its secret webhook URL once.
func (h *Handlers) CreateAlertRoute(c *okapi.Context, req *AlertRouteRequest) error {
	token := crypto.NewToken("akr")
	r := &models.AlertRoute{Base: models.Base{ID: models.NewID("alr"), OrganizationID: middlewares.OrgID(c)}, TokenHash: crypto.HashToken(token),
		TokenPrefix: token[:10], Enabled: true, Autonomy: proto.AutonomyL2, CreatedBy: middlewares.UserID(c)}
	if err := h.applyRoute(r, req); err != nil {
		return c.AbortBadRequest(err.Error())
	}
	if err := h.DB.Create(r).Error; err != nil {
		return c.AbortInternalServerError("create failed", err)
	}
	h.record(c, "alert_route.create", "alert_route", r.ID, map[string]any{"name": r.Name, "match": r.Match})
	return created(c, AlertRouteCreated{Route: r, WebhookURL: h.Cfg.PublicURL + "/api/v1/webhooks/alerts/" + token})
}

// UpdateAlertRoute edits a route.
func (h *Handlers) UpdateAlertRoute(c *okapi.Context, req *AlertRouteRequest) error {
	var r models.AlertRoute
	if err := h.DB.First(&r, "id = ? AND organization_id = ?", c.Param("id"), middlewares.OrgID(c)).Error; err != nil {
		return c.AbortNotFound("alert route not found")
	}
	if err := h.applyRoute(&r, req); err != nil {
		return c.AbortBadRequest(err.Error())
	}
	if err := h.DB.Save(&r).Error; err != nil {
		return c.AbortInternalServerError("update failed", err)
	}
	h.record(c, "alert_route.update", "alert_route", r.ID, map[string]any{"enabled": r.Enabled})
	return ok(c, r)
}

// RotateAlertRoute issues a new webhook token, invalidating the old one.
func (h *Handlers) RotateAlertRoute(c *okapi.Context) error {
	var r models.AlertRoute
	if err := h.DB.First(&r, "id = ? AND organization_id = ?", c.Param("id"), middlewares.OrgID(c)).Error; err != nil {
		return c.AbortNotFound("alert route not found")
	}
	token := crypto.NewToken("akr")
	h.DB.Model(&r).Updates(map[string]any{"token_hash": crypto.HashToken(token), "token_prefix": token[:10]})
	r.TokenPrefix = token[:10]
	h.record(c, "alert_route.rotate", "alert_route", r.ID, nil)
	return ok(c, AlertRouteCreated{Route: &r, WebhookURL: h.Cfg.PublicURL + "/api/v1/webhooks/alerts/" + token})
}

// DeleteAlertRoute removes a route.
func (h *Handlers) DeleteAlertRoute(c *okapi.Context) error {
	res := h.DB.Where("id = ? AND organization_id = ?", c.Param("id"), middlewares.OrgID(c)).Delete(&models.AlertRoute{})
	if res.RowsAffected == 0 {
		return c.AbortNotFound("alert route not found")
	}
	h.record(c, "alert_route.delete", "alert_route", c.Param("id"), nil)
	return message(c, "deleted")
}

// AlertWebhookResult says what happened to each alert.
type AlertWebhookResult struct {
	Created []string `json:"created"`
	Skipped []string `json:"skipped"`
}

// AlertWebhook receives Alertmanager (or generic) alerts on a route's secret URL.
func (h *Handlers) AlertWebhook(c *okapi.Context) error {
	ctx := c.Request().Context()
	if !auth.RateLimit(ctx, h.Bus.Redis(), "alerts:"+c.RealIP(), 120, time.Minute, false) {
		return c.AbortTooManyRequests("too many alerts")
	}
	var route models.AlertRoute
	if err := h.DB.First(&route, "token_hash = ?", crypto.HashToken(c.Param("token"))).Error; err != nil || !route.Enabled {
		return c.AbortNotFound("unknown alert route")
	}
	body, err := io.ReadAll(io.LimitReader(c.Request().Body, 1<<20))
	if err != nil {
		return c.AbortBadRequest("unreadable body")
	}
	list, err := alerts.Parse(body)
	if err != nil {
		return c.AbortBadRequest(err.Error())
	}
	now := time.Now().UTC()
	h.DB.Model(&route).Update("last_alert_at", now)
	out := AlertWebhookResult{Created: []string{}, Skipped: []string{}}
	for _, a := range list {
		if a.Status != "firing" || !a.Matches(route.Match) {
			out.Skipped = append(out.Skipped, a.Fingerprint+": "+a.Status+" or not matching")
			continue
		}
		ref := "alert:" + route.ID + ":" + a.Fingerprint
		var n int64
		h.DB.Model(&models.Task{}).Where("trigger_ref = ? AND status NOT IN ?", ref,
			[]string{models.TaskSucceeded, models.TaskFailed, models.TaskCancelled, models.TaskTimedOut}).Count(&n)
		if n > 0 {
			out.Skipped = append(out.Skipped, a.Fingerprint+": a triage task is already open")
			continue
		}
		host := a.Host(route.HostLabel)
		in := tasks.Input{Title: a.Title(host), Goal: a.Goal(route.Instructions), AgentID: route.AgentID, Selector: route.Selector,
			Priority: severityPriority(a.Severity), Autonomy: route.Autonomy, TimeoutSec: 1800, MaxAttempts: 1,
			Trigger: "alert", TriggerRef: ref}
		if agentID := h.agentForHost(ctx, route.OrganizationID, host); agentID != "" {
			in.AgentID, in.Selector = &agentID, nil
		}
		t, err := h.Tasks.Create(ctx, route.OrganizationID, "", in)
		if err != nil {
			logger.Warn("alert could not create a task", "route", route.ID, "alert", a.Name, "error", err)
			out.Skipped = append(out.Skipped, a.Fingerprint+": "+err.Error())
			continue
		}
		out.Created = append(out.Created, t.ID)
	}
	h.Audit.Best(ctx, audit.Entry{OrganizationID: route.OrganizationID, ActorType: audit.ActorSystem, Action: "alert.received",
		TargetType: "alert_route", TargetID: route.ID, IP: c.RealIP(), Metadata: map[string]any{"alerts": len(list), "tasks": out.Created}})
	return ok(c, out)
}

// agentForHost finds an agent whose name or hostname matches an alert's host.
func (h *Handlers) agentForHost(ctx context.Context, org, host string) string {
	if host == "" {
		return ""
	}
	var agents []models.Agent
	h.DB.WithContext(ctx).Where("organization_id = ? AND status <> ?", org, models.AgentRevoked).Find(&agents)
	for _, a := range agents {
		if strings.EqualFold(a.Name, host) || strings.EqualFold(a.Facts.Hostname, host) ||
			strings.EqualFold(strings.Split(a.Facts.Hostname, ".")[0], strings.Split(host, ".")[0]) {
			return a.ID
		}
	}
	return ""
}

func severityPriority(sev string) int {
	switch strings.ToLower(sev) {
	case "critical", "page", "p1":
		return 100
	case "warning", "p2":
		return 50
	}
	return 10
}

func emptyNil(s *string) *string {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	return s
}

func nonNilList(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// ---- terminals ----

// ListTerminals lists recorded terminal sessions.
func (h *Handlers) ListTerminals(c *okapi.Context) error {
	q := h.DB.Omit("recording").Where("organization_id = ?", middlewares.OrgID(c))
	if a := c.Query("agent_id"); a != "" {
		q = q.Where("agent_id = ?", a)
	}
	p := pageParams(c)
	out, total, err := pagination.Find[models.TerminalSession](q, p, "created_at DESC, id DESC")
	if err != nil {
		return c.AbortInternalServerError("list failed", err)
	}
	return paged(c, out, total, p)
}

// GetTerminal returns one terminal session (without the recording).
func (h *Handlers) GetTerminal(c *okapi.Context) error {
	var t models.TerminalSession
	if err := h.DB.Omit("recording").First(&t, "id = ? AND organization_id = ?", c.Param("id"), middlewares.OrgID(c)).Error; err != nil {
		return c.AbortNotFound("terminal session not found")
	}
	return ok(c, t)
}

// TerminalRecording returns a session's asciinema v2 cast.
func (h *Handlers) TerminalRecording(c *okapi.Context) error {
	var t models.TerminalSession
	if err := h.DB.First(&t, "id = ? AND organization_id = ?", c.Param("id"), middlewares.OrgID(c)).Error; err != nil {
		return c.AbortNotFound("terminal session not found")
	}
	h.record(c, "terminal.replay", "terminal", t.ID, nil)
	return c.Data(http.StatusOK, "application/x-asciicast", []byte(t.Recording))
}

// terminalIdle closes a terminal nobody has typed into or read from for this long.
const terminalIdle = 15 * time.Minute

// Terminal opens a recorded interactive terminal on an agent over a browser WebSocket. Browser
// frames: {"type":"input","data":"..."} and {"type":"resize","cols":N,"rows":N}. Server frames:
// binary messages are terminal output; text messages are {"type":"exit","code":N,"error":"..."}.
func (h *Handlers) Terminal(c *okapi.Context) error {
	ctx := c.Request().Context()
	org, userID := middlewares.OrgID(c), middlewares.UserID(c)
	// A root shell is too powerful for an API key of any scope: the same-origin check only stops
	// browsers, so require an interactive browser session.
	if c.GetString(middlewares.CtxAuthMethod) != "session" {
		return c.AbortForbidden("open a terminal from the web UI with a browser session")
	}
	agent, err := h.Fleet.Get(ctx, org, c.Param("id"))
	if err != nil {
		return c.AbortNotFound("agent not found")
	}
	if agent.Status != models.AgentOnline {
		return c.AbortConflict("the agent is not connected")
	}
	if pol, _, err := h.Sessions.SignedAgentPolicy(ctx, agent.ID); err != nil || !pol.Terminal {
		return c.AbortForbidden("the agent's policy does not allow terminals")
	}
	cols, rows := queryInt(c, "cols", 120), queryInt(c, "rows", 32)
	upgrader := websocket.Upgrader{CheckOrigin: h.sameOrigin, ReadBufferSize: 32 << 10, WriteBufferSize: 32 << 10}
	ws, err := upgrader.Upgrade(c.ResponseWriter(), c.Request(), nil)
	if err != nil {
		return nil
	}
	defer ws.Close()
	ts := models.TerminalSession{Base: models.Base{ID: models.NewID("trm"), OrganizationID: org}, AgentID: agent.ID, UserID: userID,
		Status: "open", Cols: cols, Rows: rows}
	if err := h.DB.Create(&ts).Error; err != nil {
		return nil
	}
	// An interactive shell on a server is on the record before it opens.
	if err := h.Audit.Record(ctx, audit.Entry{OrganizationID: org, ActorType: audit.ActorUser, ActorID: userID, Action: "terminal.open",
		TargetType: "terminal", TargetID: ts.ID, IP: c.RealIP(), Metadata: map[string]any{"agent_id": agent.ID, "agent": agent.Name}}); err != nil {
		_ = ws.WriteJSON(map[string]any{"type": "exit", "code": -1, "error": "audit trail unavailable"})
		return nil
	}
	frames, stop := h.Bus.SubscribeTerminal(context.Background(), ts.ID)
	defer stop()
	send := func(cmd string, data any) error {
		return h.Bus.SendCommandData(context.Background(), agent.ID, bus.Command{Type: cmd, SessionID: ts.ID}, data)
	}
	if err := send(bus.CmdTerminalOpen, bus.TerminalOpen{Cols: cols, Rows: rows, UserID: userID}); err != nil {
		_ = ws.WriteJSON(map[string]any{"type": "exit", "code": -1, "error": "the agent is not connected"})
		h.DB.Model(&ts).Updates(map[string]any{"status": "closed", "ended_at": time.Now().UTC()})
		return nil
	}
	defer func() { _ = send(bus.CmdTerminalClose, nil) }()

	var wmu sync.Mutex
	activity := make(chan struct{}, 1)
	touch := func() {
		select {
		case activity <- struct{}{}:
		default:
		}
	}
	go func() { // agent → browser
		for f := range frames {
			touch()
			wmu.Lock()
			var err error
			if f.Type == "output" {
				err = ws.WriteMessage(websocket.BinaryMessage, f.Data)
			} else {
				err = ws.WriteJSON(map[string]any{"type": "exit", "code": f.Code, "error": f.Err})
			}
			wmu.Unlock()
			if err != nil || f.Type == "exit" {
				_ = ws.Close()
				return
			}
		}
	}()
	go func() { // idle timeout
		t := time.NewTimer(terminalIdle)
		defer t.Stop()
		for {
			select {
			case <-activity:
				if !t.Stop() {
					<-t.C
				}
				t.Reset(terminalIdle)
			case <-t.C:
				wmu.Lock()
				_ = ws.WriteJSON(map[string]any{"type": "exit", "code": -1, "error": "closed after 15 minutes of inactivity"})
				wmu.Unlock()
				_ = ws.Close()
				return
			}
		}
	}()
	ws.SetReadLimit(64 << 10)
	for { // browser → agent
		_, msg, err := ws.ReadMessage()
		if err != nil {
			return nil
		}
		touch()
		var in struct {
			Type string `json:"type"`
			Data string `json:"data"`
			Cols int    `json:"cols"`
			Rows int    `json:"rows"`
		}
		if json.Unmarshal(msg, &in) != nil {
			continue
		}
		switch in.Type {
		case "input":
			_ = send(bus.CmdTerminalInput, proto.PTYData{Data: []byte(in.Data)})
		case "resize":
			_ = send(bus.CmdTerminalResize, proto.PTYResize{Cols: in.Cols, Rows: in.Rows})
		}
	}
}

// sameOrigin blocks cross-site WebSocket hijacking: the browser must be on this host or an allowed origin.
func (h *Handlers) sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}
	for _, o := range h.Cfg.CORSOrigins {
		if strings.EqualFold(strings.TrimRight(o, "/"), strings.TrimRight(origin, "/")) {
			return true
		}
	}
	if pu, err := url.Parse(h.Cfg.PublicURL); err == nil && strings.EqualFold(pu.Host, u.Host) {
		return true
	}
	return false
}
