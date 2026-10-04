// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package handlers

import (
	"strings"
	"time"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/bus"
	"github.com/goakili/akili/server/internal/middlewares"
	"github.com/goakili/akili/server/internal/models"
	"github.com/goakili/akili/server/internal/tasks"
	"github.com/jkaninda/okapi"
)

type busEvent = bus.Event

// TaskRequest creates a task.
type TaskRequest struct {
	Body struct {
		Title       string          `json:"title" maxLength:"200"`
		Goal        string          `json:"goal" required:"true"`
		AgentID     *string         `json:"agent_id"`
		Selector    []string        `json:"selector"`
		Priority    int             `json:"priority"`
		Autonomy    *proto.Autonomy `json:"autonomy" description:"0-3; default L2 for project tasks, L1 otherwise"`
		BudgetUSD   float64         `json:"budget_usd"`
		MaxTurns    int             `json:"max_turns"`
		TimeoutSec  int             `json:"timeout_sec"`
		MaxAttempts int             `json:"max_attempts"`
		ProjectID   *string         `json:"project_id" description:"work on a project's repository (branch, commits, pull request)"`
		PlanIDs     []string        `json:"plan_ids" maxItems:"10" description:"project plans the task works on; they must be active and in the task's project"`
		PlanPhaseID string          `json:"plan_phase_id" description:"focus the task on one phase of a plan; the plan is linked too"`
		Draft       bool            `json:"draft" description:"save without queueing; POST /tasks/{id}/start queues it"`
	} `json:"body"`
}

// ListTasks lists tasks.
func (h *Handlers) ListTasks(c *okapi.Context) error {
	ctx := c.Request().Context()
	out, err := h.Tasks.List(ctx, middlewares.OrgID(c), c.Query("status"), queryInt(c, "limit", 200), c.Query("project_id"))
	if err != nil {
		return c.AbortInternalServerError("list failed", err)
	}
	ids := make([]string, len(out))
	for i := range out {
		ids[i] = out[i].ID
	}
	links := h.Plans.PlanIDs(ctx, middlewares.OrgID(c), ids)
	for i := range out {
		out[i].PlanIDs = links[out[i].ID]
	}
	return ok(c, out)
}

// CreateTask queues a task.
func (h *Handlers) CreateTask(c *okapi.Context, req *TaskRequest) error {
	b := req.Body
	autonomy := tasks.DefaultAutonomy(b.ProjectID)
	if b.Autonomy != nil {
		autonomy = *b.Autonomy
	}
	t, err := h.Tasks.Create(c.Request().Context(), middlewares.OrgID(c), middlewares.UserID(c), tasks.Input{
		Title: strings.TrimSpace(b.Title), Goal: b.Goal, AgentID: b.AgentID, Selector: b.Selector, Priority: b.Priority,
		Autonomy: autonomy, BudgetUSD: b.BudgetUSD, MaxTurns: b.MaxTurns, TimeoutSec: b.TimeoutSec, MaxAttempts: b.MaxAttempts, ProjectID: b.ProjectID,
		PlanIDs: b.PlanIDs, PlanPhaseID: b.PlanPhaseID, Draft: b.Draft})
	if err != nil {
		return mapErr(c, err)
	}
	return created(c, t)
}

// GetTask returns a task.
func (h *Handlers) GetTask(c *okapi.Context) error {
	t, err := h.Tasks.Get(c.Request().Context(), middlewares.OrgID(c), c.Param("id"))
	if err != nil {
		return mapErr(c, err)
	}
	t.PlanIDs = h.Plans.PlanIDs(c.Request().Context(), middlewares.OrgID(c), []string{t.ID})[t.ID]
	return ok(c, t)
}

// StartTask queues a draft task.
func (h *Handlers) StartTask(c *okapi.Context) error {
	t, err := h.Tasks.Start(c.Request().Context(), middlewares.OrgID(c), middlewares.UserID(c), c.Param("id"))
	if err != nil {
		return mapErr(c, err)
	}
	return ok(c, t)
}

// TaskPlans returns the plans linked to a task, as the agent received them.
func (h *Handlers) TaskPlans(c *okapi.Context) error {
	t, err := h.Tasks.Get(c.Request().Context(), middlewares.OrgID(c), c.Param("id"))
	if err != nil {
		return mapErr(c, err)
	}
	return ok(c, h.Plans.ForTask(c.Request().Context(), t.OrganizationID, t.ID))
}

// CancelTask stops a task.
func (h *Handlers) CancelTask(c *okapi.Context) error {
	t, err := h.Tasks.Cancel(c.Request().Context(), middlewares.OrgID(c), middlewares.UserID(c), c.Param("id"))
	if err != nil {
		return mapErr(c, err)
	}
	return ok(c, t)
}

// RetryTask queues a finished task again.
func (h *Handlers) RetryTask(c *okapi.Context) error {
	t, err := h.Tasks.Retry(c.Request().Context(), middlewares.OrgID(c), middlewares.UserID(c), c.Param("id"))
	if err != nil {
		return mapErr(c, err)
	}
	return created(c, t)
}

// ---- schedules -----------------------------------------------------------------------------------

// ScheduleRequest creates or updates a schedule.
type ScheduleRequest struct {
	Body struct {
		Name     string              `json:"name" required:"true"`
		Cron     string              `json:"cron" required:"true"`
		Enabled  bool                `json:"enabled"`
		Template models.TaskTemplate `json:"template"`
	} `json:"body"`
}

// ListSchedules lists schedules.
func (h *Handlers) ListSchedules(c *okapi.Context) error {
	var out []models.Schedule
	h.DB.Where("organization_id = ?", middlewares.OrgID(c)).Order("name").Find(&out)
	return ok(c, out)
}

func (h *Handlers) validateSchedule(c *okapi.Context, req *ScheduleRequest) (*time.Time, error) {
	next, err := tasks.NextRun(req.Body.Cron, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Body.Template.Goal) == "" {
		return nil, errBad("template.goal is required")
	}
	if !req.Body.Template.Autonomy.Valid() {
		return nil, errBad("template.autonomy must be 0-3")
	}
	return &next, nil
}

// CreateSchedule adds a schedule.
func (h *Handlers) CreateSchedule(c *okapi.Context, req *ScheduleRequest) error {
	next, err := h.validateSchedule(c, req)
	if err != nil {
		return c.AbortBadRequest(err.Error())
	}
	s := models.Schedule{Base: models.Base{ID: models.NewID("sch"), OrganizationID: middlewares.OrgID(c)}, Name: req.Body.Name, Cron: req.Body.Cron,
		Enabled: req.Body.Enabled, Template: req.Body.Template, NextRunAt: next, CreatedBy: middlewares.UserID(c)}
	if err := h.DB.Create(&s).Error; err != nil {
		return c.AbortInternalServerError("create failed", err)
	}
	h.record(c, "schedule.create", "schedule", s.ID, map[string]any{"name": s.Name, "cron": s.Cron})
	return created(c, s)
}

// UpdateSchedule edits a schedule.
func (h *Handlers) UpdateSchedule(c *okapi.Context, req *ScheduleRequest) error {
	var s models.Schedule
	if err := h.DB.First(&s, "id = ? AND organization_id = ?", c.Param("id"), middlewares.OrgID(c)).Error; err != nil {
		return c.AbortNotFound("schedule not found")
	}
	next, err := h.validateSchedule(c, req)
	if err != nil {
		return c.AbortBadRequest(err.Error())
	}
	s.Name, s.Cron, s.Enabled, s.Template, s.NextRunAt = req.Body.Name, req.Body.Cron, req.Body.Enabled, req.Body.Template, next
	if err := h.DB.Save(&s).Error; err != nil {
		return c.AbortInternalServerError("update failed", err)
	}
	h.record(c, "schedule.update", "schedule", s.ID, map[string]any{"cron": s.Cron, "enabled": s.Enabled})
	return ok(c, s)
}

// DeleteSchedule removes a schedule.
func (h *Handlers) DeleteSchedule(c *okapi.Context) error {
	res := h.DB.Where("id = ? AND organization_id = ?", c.Param("id"), middlewares.OrgID(c)).Delete(&models.Schedule{})
	if res.RowsAffected == 0 {
		return c.AbortNotFound("schedule not found")
	}
	h.record(c, "schedule.delete", "schedule", c.Param("id"), nil)
	return message(c, "deleted")
}

// RunSchedule creates a task from a schedule now.
func (h *Handlers) RunSchedule(c *okapi.Context) error {
	var s models.Schedule
	if err := h.DB.First(&s, "id = ? AND organization_id = ?", c.Param("id"), middlewares.OrgID(c)).Error; err != nil {
		return c.AbortNotFound("schedule not found")
	}
	t, err := h.Tasks.Fire(c.Request().Context(), &s)
	if err != nil {
		return mapErr(c, err)
	}
	h.record(c, "schedule.run", "schedule", s.ID, map[string]any{"task_id": t.ID})
	return created(c, t)
}

type badRequest string

func (e badRequest) Error() string { return string(e) }

func errBad(s string) error { return badRequest(s) }
