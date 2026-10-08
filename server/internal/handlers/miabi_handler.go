// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package handlers

import (
	"context"
	"errors"
	"io"
	"path"
	"strings"
	"time"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/auth"
	"github.com/goakili/akili/server/internal/miabi"
	"github.com/goakili/akili/server/internal/middlewares"
	"github.com/goakili/akili/server/internal/models"
	"github.com/goakili/akili/server/internal/tasks"
	"github.com/jkaninda/logger"
	"github.com/jkaninda/okapi"
)

// MiabiWatchRequest creates or updates a Miabi watch.
type MiabiWatchRequest struct {
	Body struct {
		IntegrationID  string          `json:"integration_id" required:"true"`
		Workspace      string          `json:"workspace" description:"Miabi workspace name; default: the integration's only enabled workspace"`
		App            string          `json:"app" required:"true" description:"an app name, or a pattern such as \"*\" (every app) or \"api-*\""`
		Databases      *bool           `json:"databases" description:"triage failed backups, restores and database provisioning or upgrades (default false)"`
		AgentID        *string         `json:"agent_id"`
		Selector       []string        `json:"selector"`
		VerifyDeploys  *bool           `json:"verify_deploys" description:"check every successful deploy and roll back an unhealthy one (default true)"`
		TriageFailures *bool           `json:"triage_failures" description:"open a triage task on failed deploys and crashed containers (default true)"`
		HealthURL      string          `json:"health_url"`
		Autonomy       *proto.Autonomy `json:"autonomy" description:"default L2; a rollback still needs an approved change plan"`
		Instructions   string          `json:"instructions"`
	} `json:"body"`
}

// ListMiabiWatches lists watched Miabi apps.
func (h *Handlers) ListMiabiWatches(c *okapi.Context) error {
	var out []models.MiabiWatch
	h.DB.Where("organization_id = ?", middlewares.OrgID(c)).Order("app").Find(&out)
	return ok(c, out)
}

func (h *Handlers) saveWatch(c *okapi.Context, w *models.MiabiWatch, req *MiabiWatchRequest) error {
	b := req.Body
	ctx := c.Request().Context()
	var it models.Integration
	if err := h.DB.First(&it, "id = ? AND organization_id = ? AND kind = ?", b.IntegrationID, middlewares.OrgID(c), models.KindMiabi).Error; err != nil {
		return c.AbortBadRequest("unknown Miabi integration")
	}
	ws := strings.TrimSpace(b.Workspace)
	if ws == "" {
		var rows []models.MiabiWorkspace
		h.DB.Where("integration_id = ? AND enabled AND accessible", it.ID).Find(&rows)
		if len(rows) != 1 {
			return c.AbortBadRequest("choose a workspace: the integration has several (or no) enabled workspaces")
		}
		ws = rows[0].Name
	}
	pattern := strings.TrimSpace(b.App)
	appID := int64(0)
	if strings.ContainsAny(pattern, "*?[") {
		if _, err := path.Match(pattern, "x"); err != nil {
			return c.AbortBadRequest("invalid app pattern")
		}
		// Make sure the workspace is usable even though no single app is checked.
		if _, err := h.Miabi.Apps(ctx, &it, ws); err != nil {
			return c.AbortBadRequest("Miabi: " + err.Error())
		}
	} else {
		app, err := h.Miabi.App(ctx, &it, ws, pattern)
		if err != nil {
			return c.AbortBadRequest("Miabi: " + err.Error())
		}
		pattern, appID = app.Name, app.ID
	}
	w.IntegrationID, w.Workspace, w.App, w.AppID = it.ID, ws, pattern, appID
	w.AgentID, w.Selector, w.HealthURL, w.Instructions = emptyNil(b.AgentID), nonNilList(b.Selector), strings.TrimSpace(b.HealthURL), b.Instructions
	if b.VerifyDeploys != nil {
		w.VerifyDeploys = *b.VerifyDeploys
	}
	if b.TriageFailures != nil {
		w.TriageFailures = *b.TriageFailures
	}
	if b.Databases != nil {
		w.Databases = *b.Databases
	}
	if b.Autonomy != nil {
		if !b.Autonomy.Valid() {
			return c.AbortBadRequest("autonomy must be 0-3")
		}
		w.Autonomy = *b.Autonomy
	}
	return h.DB.Save(w).Error
}

// CreateMiabiWatch starts watching a Miabi app.
func (h *Handlers) CreateMiabiWatch(c *okapi.Context, req *MiabiWatchRequest) error {
	w := &models.MiabiWatch{Base: models.Base{ID: models.NewID("mbw"), OrganizationID: middlewares.OrgID(c)}, VerifyDeploys: true, TriageFailures: true,
		Autonomy: proto.AutonomyL2, CreatedBy: middlewares.UserID(c)}
	if err := h.saveWatch(c, w, req); err != nil {
		return err
	}
	h.record(c, "miabi_watch.create", "miabi_watch", w.ID, map[string]any{"app": w.App, "integration_id": w.IntegrationID})
	return created(c, w)
}

// UpdateMiabiWatch edits a watch.
func (h *Handlers) UpdateMiabiWatch(c *okapi.Context, req *MiabiWatchRequest) error {
	var w models.MiabiWatch
	if err := h.DB.First(&w, "id = ? AND organization_id = ?", c.Param("id"), middlewares.OrgID(c)).Error; err != nil {
		return c.AbortNotFound("watch not found")
	}
	if err := h.saveWatch(c, &w, req); err != nil {
		return err
	}
	h.record(c, "miabi_watch.update", "miabi_watch", w.ID, nil)
	return ok(c, w)
}

// DeleteMiabiWatch stops watching an app.
func (h *Handlers) DeleteMiabiWatch(c *okapi.Context) error {
	res := h.DB.Where("id = ? AND organization_id = ?", c.Param("id"), middlewares.OrgID(c)).Delete(&models.MiabiWatch{})
	if res.RowsAffected == 0 {
		return c.AbortNotFound("watch not found")
	}
	h.record(c, "miabi_watch.delete", "miabi_watch", c.Param("id"), nil)
	return message(c, "deleted")
}

// MiabiWebhook receives Miabi's signed outbound webhooks and turns watched events into tasks.
func (h *Handlers) MiabiWebhook(c *okapi.Context) error {
	ctx := c.Request().Context()
	if !auth.RateLimit(ctx, h.Bus.Redis(), "miabi-hook:"+c.RealIP(), 300, time.Minute, false) {
		return c.AbortTooManyRequests("too many events")
	}
	body, err := io.ReadAll(io.LimitReader(c.Request().Body, 1<<20))
	if err != nil {
		return c.AbortBadRequest("unreadable body")
	}
	trigs, err := h.Miabi.ParseWebhook(ctx, c.Param("id"), c.Request().Header, body)
	switch {
	case errors.Is(err, miabi.ErrIgnored):
		return message(c, "ignored")
	case errors.Is(err, miabi.ErrBadSignature):
		return c.AbortUnauthorized("invalid webhook signature")
	case err != nil:
		return c.AbortBadRequest(err.Error())
	}
	var out []*models.Task
	for i := range trigs {
		if t := h.MiabiTrigger(ctx, &trigs[i]); t != nil {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return message(c, "a task for this event is already open")
	}
	return created(c, out)
}

// MiabiTrigger opens the task an event asks for, once: webhooks and the event stream can both
// deliver the same event, and a failing app keeps reporting until someone looks.
func (h *Handlers) MiabiTrigger(ctx context.Context, trig *miabi.Trigger) *models.Task {
	if !h.Bus.Redis().SetNX(ctx, "akili:miabi-trigger:"+trig.Ref, 1, time.Minute).Val() {
		return nil
	}
	var n int64
	h.DB.Model(&models.Task{}).Where("trigger_ref = ? AND status NOT IN ?", trig.Ref,
		[]string{models.TaskSucceeded, models.TaskFailed, models.TaskCancelled, models.TaskTimedOut}).Count(&n)
	if n > 0 {
		return nil
	}
	w := trig.Watch
	t, err := h.Tasks.Create(ctx, w.OrganizationID, "", tasks.Input{Title: trig.Title, Goal: trig.Goal, AgentID: w.AgentID, Selector: w.Selector,
		Priority: 80, Autonomy: w.Autonomy, TimeoutSec: 2400, MaxAttempts: 1, Trigger: "miabi", TriggerRef: trig.Ref})
	if err != nil {
		logger.Warn("Miabi event could not create a task", "watch", w.ID, "error", err)
		return nil
	}
	return t
}

// ListMiabiWorkspaces lists the workspaces a Miabi integration's key can reach.
func (h *Handlers) ListMiabiWorkspaces(c *okapi.Context) error {
	var it models.Integration
	if err := h.DB.First(&it, "id = ? AND organization_id = ? AND kind = ?", c.Param("id"), middlewares.OrgID(c), models.KindMiabi).Error; err != nil {
		return c.AbortNotFound("Miabi integration not found")
	}
	var rows []models.MiabiWorkspace
	h.DB.Where("integration_id = ?", it.ID).Order("name").Find(&rows)
	for i := range rows {
		rows[i].Streaming = h.Miabi.Streaming(rows[i].ID)
	}
	return ok(c, rows)
}

// SyncMiabiWorkspaces re-reads the workspaces from Miabi.
func (h *Handlers) SyncMiabiWorkspaces(c *okapi.Context) error {
	var it models.Integration
	if err := h.DB.First(&it, "id = ? AND organization_id = ? AND kind = ?", c.Param("id"), middlewares.OrgID(c), models.KindMiabi).Error; err != nil {
		return c.AbortNotFound("Miabi integration not found")
	}
	rows, err := h.Miabi.SyncWorkspaces(c.Request().Context(), &it)
	if err != nil {
		return c.AbortBadRequest("Miabi: " + err.Error())
	}
	h.record(c, "miabi_workspace.sync", "integration", it.ID, map[string]any{"workspaces": len(rows)})
	return ok(c, rows)
}

// MiabiWorkspaceRequest enables or disables a workspace for agents and event watching.
type MiabiWorkspaceRequest struct {
	Body struct {
		Enabled bool `json:"enabled"`
	} `json:"body"`
}

// UpdateMiabiWorkspace enables or disables a workspace.
func (h *Handlers) UpdateMiabiWorkspace(c *okapi.Context, req *MiabiWorkspaceRequest) error {
	var row models.MiabiWorkspace
	if err := h.DB.First(&row, "id = ? AND integration_id = ? AND organization_id = ?", c.Param("ws"), c.Param("id"), middlewares.OrgID(c)).Error; err != nil {
		return c.AbortNotFound("workspace not found")
	}
	if req.Body.Enabled && !row.Accessible {
		return c.AbortBadRequest("the integration's key cannot reach this workspace (it is bound to another one)")
	}
	h.DB.Model(&row).Update("enabled", req.Body.Enabled)
	row.Enabled = req.Body.Enabled
	h.record(c, "miabi_workspace.update", "miabi_workspace", row.ID, map[string]any{"name": row.Name, "enabled": row.Enabled})
	return ok(c, row)
}
