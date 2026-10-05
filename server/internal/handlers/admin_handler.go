// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package handlers

import (
	"time"

	"github.com/goakili/akili/server/internal/bus"
	"github.com/goakili/akili/server/internal/config"
	"github.com/goakili/akili/server/internal/middlewares"
	"github.com/goakili/akili/server/internal/models"
	"github.com/goakili/akili/server/internal/storage/pagination"
	"github.com/jkaninda/okapi"
)

// ListAudit pages through the audit log with optional filters.
func (h *Handlers) ListAudit(c *okapi.Context) error {
	q := h.DB.Where("organization_id = ?", middlewares.OrgID(c))
	if a := c.Query("action"); a != "" {
		q = q.Where("action LIKE ?", a+"%")
	}
	if a := c.Query("actor_id"); a != "" {
		q = q.Where("actor_id = ?", a)
	}
	if t := c.Query("target_id"); t != "" {
		q = q.Where("target_id = ?", t)
	}
	p := pageParams(c)
	out, total, err := pagination.Find[models.AuditLog](q, p, "id DESC")
	if err != nil {
		return c.AbortInternalServerError("list failed", err)
	}
	return paged(c, out, total, p)
}

// VerifyAudit walks the hash chain.
func (h *Handlers) VerifyAudit(c *okapi.Context) error {
	res, err := h.Audit.Verify(c.Request().Context())
	if err != nil {
		return c.AbortInternalServerError("verification failed", err)
	}
	h.record(c, "audit.verify", "audit", "", map[string]any{"valid": res.Valid, "checked": res.Checked})
	return ok(c, res)
}

// Overview is the dashboard summary.
type Overview struct {
	Agents struct {
		Total   int64 `json:"total"`
		Online  int64 `json:"online"`
		Pending int64 `json:"pending"`
	} `json:"agents"`
	Tasks struct {
		Queued    int64 `json:"queued"`
		Running   int64 `json:"running"`
		Succeeded int64 `json:"succeeded_24h"`
		Failed    int64 `json:"failed_24h"`
	} `json:"tasks"`
	PendingApprovals int64   `json:"pending_approvals"`
	SpendTodayUSD    float64 `json:"spend_today_usd"`
	SpendMonthUSD    float64 `json:"spend_month_usd"`
	TokensToday      int64   `json:"tokens_today"`
	KillSwitch       bool    `json:"kill_switch"`
	Version          string  `json:"version"`
}

// GetOverview returns dashboard counts.
func (h *Handlers) GetOverview(c *okapi.Context) error {
	org := middlewares.OrgID(c)
	var o Overview
	agents, _ := h.Fleet.List(c.Request().Context(), org)
	o.Agents.Total = int64(len(agents))
	for _, a := range agents {
		switch a.Status {
		case models.AgentOnline:
			o.Agents.Online++
		case models.AgentPending:
			o.Agents.Pending++
		}
	}
	day := time.Now().UTC().Add(-24 * time.Hour)
	h.DB.Model(&models.Task{}).Where("organization_id = ? AND status = ?", org, models.TaskQueued).Count(&o.Tasks.Queued)
	h.DB.Model(&models.Task{}).Where("organization_id = ? AND status IN ?", org, []string{models.TaskAssigned, models.TaskRunning}).Count(&o.Tasks.Running)
	h.DB.Model(&models.Task{}).Where("organization_id = ? AND status = ? AND finished_at > ?", org, models.TaskSucceeded, day).Count(&o.Tasks.Succeeded)
	h.DB.Model(&models.Task{}).Where("organization_id = ? AND status IN ? AND finished_at > ?", org, []string{models.TaskFailed, models.TaskTimedOut}, day).Count(&o.Tasks.Failed)
	h.DB.Model(&models.Approval{}).Where("organization_id = ? AND status = ?", org, models.ApprovalPending).Count(&o.PendingApprovals)
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	h.DB.Model(&models.Usage{}).Where("organization_id = ? AND created_at >= ?", org, today).Select("COALESCE(SUM(cost_usd),0)").Scan(&o.SpendTodayUSD)
	h.DB.Model(&models.Usage{}).Where("organization_id = ? AND created_at >= ?", org, month).Select("COALESCE(SUM(cost_usd),0)").Scan(&o.SpendMonthUSD)
	h.DB.Model(&models.Usage{}).Where("organization_id = ? AND created_at >= ?", org, today).Select("COALESCE(SUM(input_tokens + output_tokens),0)").Scan(&o.TokensToday)
	var orgRow models.Organization
	h.DB.First(&orgRow, "id = ?", org)
	o.KillSwitch = orgRow.KillSwitch
	o.Version = config.Version
	return ok(c, o)
}

// UsageRow is spend grouped by agent and model.
type UsageRow struct {
	AgentID      string  `json:"agent_id"`
	Model        string  `json:"model"`
	Calls        int64   `json:"calls"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
}

// GetUsage returns spend over the last N days (default 30), by agent and model.
func (h *Handlers) GetUsage(c *okapi.Context) error {
	days := queryInt(c, "days", 30)
	if days <= 0 || days > 365 {
		days = 30
	}
	var rows []UsageRow
	h.DB.Model(&models.Usage{}).Where("organization_id = ? AND created_at >= ?", middlewares.OrgID(c), time.Now().UTC().AddDate(0, 0, -days)).
		Select("agent_id, model, COUNT(*) AS calls, SUM(input_tokens) AS input_tokens, SUM(output_tokens) AS output_tokens, SUM(cost_usd) AS cost_usd").
		Group("agent_id, model").Order("cost_usd DESC").Scan(&rows)
	return ok(c, rows)
}

// KillSwitchRequest engages or releases the kill switch.
type KillSwitchRequest struct {
	Body struct {
		Enabled bool `json:"enabled"`
	} `json:"body"`
}

// SetKillSwitch stops every model call and tool call in the organization, and interrupts every
// running session. Releasing it lets agents continue on new input.
func (h *Handlers) SetKillSwitch(c *okapi.Context, req *KillSwitchRequest) error {
	ctx := c.Request().Context()
	org := middlewares.OrgID(c)
	if err := h.DB.Model(&models.Organization{}).Where("id = ?", org).Update("kill_switch", req.Body.Enabled).Error; err != nil {
		return c.AbortInternalServerError("update failed", err)
	}
	// Record before acting: the kill switch is the one action that must always be on the record.
	if err := h.Audit.Record(ctx, auditEntry(c, "system.kill_switch", map[string]any{"enabled": req.Body.Enabled})); err != nil {
		return c.AbortInternalServerError("audit unavailable", err)
	}
	if req.Body.Enabled {
		var open []models.ChatSession
		h.DB.Where("organization_id = ? AND status = ? AND state <> ?", org, models.SessionOpen, "idle").Find(&open)
		for _, s := range open {
			_ = h.Bus.SendCommand(ctx, s.AgentID, bus.Command{Type: bus.CmdInterrupt, SessionID: s.ID})
		}
	}
	h.Bus.EmitData(ctx, org, bus.Event{Type: "system.kill_switch"}, map[string]bool{"enabled": req.Body.Enabled})
	return ok(c, map[string]bool{"enabled": req.Body.Enabled})
}

// Health is the liveness/readiness response.
type Health struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// Healthz is the liveness probe.
func (h *Handlers) Healthz(c *okapi.Context) error {
	return c.JSON(200, Health{Status: "ok", Version: config.Version})
}

// Readyz checks the database and Redis.
func (h *Handlers) Readyz(c *okapi.Context) error {
	ctx := c.Request().Context()
	sqlDB, err := h.DB.DB()
	if err == nil {
		err = sqlDB.PingContext(ctx)
	}
	if err == nil {
		err = h.Bus.Redis().Ping(ctx).Err()
	}
	if err != nil {
		return c.JSON(503, Health{Status: "unavailable", Version: config.Version})
	}
	return c.JSON(200, Health{Status: "ready", Version: config.Version})
}
