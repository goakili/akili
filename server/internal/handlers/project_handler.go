// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package handlers

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/coder"
	"github.com/goakili/akili/server/internal/middlewares"
	"github.com/goakili/akili/server/internal/models"
	"github.com/goakili/akili/server/internal/tasks"
	"github.com/jkaninda/logger"
	"github.com/jkaninda/okapi"
	"gorm.io/gorm"
)

// ---- integrations ----

// IntegrationRequest creates or updates a forge integration. Secrets are write-only.
type IntegrationRequest struct {
	Body struct {
		Name           string  `json:"name" required:"true"`
		Kind           string  `json:"kind" required:"true" description:"gitea, github, miabi or posta"`
		BaseURL        string  `json:"base_url" description:"Gitea URL, or the GitHub API URL (default https://api.github.com)"`
		WebURL         string  `json:"web_url" description:"GitHub web URL (default https://github.com)"`
		AuthType       string  `json:"auth_type" description:"token (default) or github_app"`
		Username       string  `json:"username" description:"token owner (Gitea)"`
		Token          string  `json:"token"`
		AppID          int64   `json:"app_id"`
		InstallationID int64   `json:"installation_id"`
		PrivateKey     string  `json:"private_key"`
		WebhookSecret  string  `json:"webhook_secret" description:"forge webhook secret, or the secret Miabi shows when you create its outbound webhook"`
		Workspace      string  `json:"workspace" description:"Miabi workspace id, uid or handle (kind miabi)"`
		CACert         *string `json:"ca_cert" description:"Miabi or Posta: CA certificate (PEM) for a self-signed or private-CA server; omit to keep, empty to remove"`
		Default        *bool   `json:"default" description:"Miabi or Posta: the integration used when none is named; omit to keep"`
		Sender         string  `json:"sender" description:"Posta: From address of notification email, e.g. Akili <akili@example.com>"`
	} `json:"body"`
}

func (r *IntegrationRequest) input() coder.IntegrationInput {
	b := r.Body
	return coder.IntegrationInput{Name: b.Name, Kind: b.Kind, BaseURL: b.BaseURL, WebURL: b.WebURL, AuthType: b.AuthType, Username: b.Username,
		Token: b.Token, AppID: b.AppID, InstallationID: b.InstallationID, PrivateKey: b.PrivateKey, WebhookSecret: b.WebhookSecret, Workspace: b.Workspace, CACert: b.CACert, Default: b.Default,
		Sender: b.Sender}
}

// IntegrationView adds the webhook URL to an integration.
type IntegrationView struct {
	models.Integration
	WebhookURL string `json:"webhook_url"`
}

func (h *Handlers) integrationView(it models.Integration) IntegrationView {
	path := "/api/v1/webhooks/forge/"
	if it.Kind == models.KindMiabi {
		path = "/api/v1/webhooks/miabi/"
	}
	return IntegrationView{Integration: it, WebhookURL: h.Cfg.PublicURL + path + it.ID}
}

// ListIntegrations lists forge integrations.
func (h *Handlers) ListIntegrations(c *okapi.Context) error {
	var list []models.Integration
	h.DB.Where("organization_id = ?", middlewares.OrgID(c)).Order("name").Find(&list)
	out := make([]IntegrationView, 0, len(list))
	for _, it := range list {
		out = append(out, h.integrationView(it))
	}
	return ok(c, out)
}

// CreateIntegration adds a forge integration.
func (h *Handlers) CreateIntegration(c *okapi.Context, req *IntegrationRequest) error {
	it := &models.Integration{Base: models.Base{ID: models.NewID("int"), OrganizationID: middlewares.OrgID(c)}, CreatedBy: middlewares.UserID(c)}
	if err := h.Coder.SaveIntegration(c.Request().Context(), it, req.input()); err != nil {
		return c.AbortBadRequest(err.Error())
	}
	h.record(c, "integration.create", "integration", it.ID, map[string]any{"name": it.Name, "kind": it.Kind, "auth_type": it.AuthType})
	if it.Kind == models.KindMiabi {
		// Best effort: list the workspaces now; "Test" or "Sync" retries if Miabi is unreachable.
		if _, err := h.Miabi.SyncWorkspaces(c.Request().Context(), it); err != nil {
			logger.Warn("miabi workspace sync failed", "integration", it.ID, "error", err)
		}
	}
	h.refreshTokenInfo(c, it)
	return created(c, h.integrationView(*it))
}

// UpdateIntegration edits an integration; empty secrets keep the stored ones.
func (h *Handlers) UpdateIntegration(c *okapi.Context, req *IntegrationRequest) error {
	var it models.Integration
	if err := h.DB.First(&it, "id = ? AND organization_id = ?", c.Param("id"), middlewares.OrgID(c)).Error; err != nil {
		return c.AbortNotFound("integration not found")
	}
	if err := h.Coder.SaveIntegration(c.Request().Context(), &it, req.input()); err != nil {
		return c.AbortBadRequest(err.Error())
	}
	h.record(c, "integration.update", "integration", it.ID, map[string]any{"secret_changed": req.Body.Token != "" || req.Body.PrivateKey != "", "default": it.Default})
	h.refreshTokenInfo(c, &it)
	return ok(c, h.integrationView(it))
}

// refreshTokenInfo reads a GitLab token's kind and expiry; best effort, "Test" retries.
func (h *Handlers) refreshTokenInfo(c *okapi.Context, it *models.Integration) {
	if it.Kind != models.ForgeGitLab {
		return
	}
	if err := h.Coder.RefreshTokenInfo(c.Request().Context(), it); err != nil {
		logger.Warn("gitlab token check failed", "integration", it.ID, "error", err)
	}
}

// SetDefaultIntegration makes a Miabi integration the default for tool calls that name none.
func (h *Handlers) SetDefaultIntegration(c *okapi.Context) error {
	it, err := h.Coder.SetDefault(c.Request().Context(), middlewares.OrgID(c), c.Param("id"))
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return c.AbortNotFound("integration not found")
	case errors.Is(err, coder.ErrNotMiabi):
		return c.AbortBadRequest(err.Error())
	case err != nil:
		return c.AbortInternalServerError("could not set the default integration", err)
	}
	h.record(c, "integration.set_default", "integration", it.ID, map[string]any{"name": it.Name, "kind": it.Kind})
	return ok(c, h.integrationView(*it))
}

// DeleteIntegration removes an integration that no project uses.
func (h *Handlers) DeleteIntegration(c *okapi.Context) error {
	var n int64
	h.DB.Model(&models.Project{}).Where("integration_id = ?", c.Param("id")).Count(&n)
	if n > 0 {
		return c.AbortConflict("the integration is used by projects")
	}
	res := h.DB.Where("id = ? AND organization_id = ?", c.Param("id"), middlewares.OrgID(c)).Delete(&models.Integration{})
	if res.RowsAffected == 0 {
		return c.AbortNotFound("integration not found")
	}
	h.record(c, "integration.delete", "integration", c.Param("id"), nil)
	return message(c, "deleted")
}

// TestIntegration checks an integration's credentials.
func (h *Handlers) TestIntegration(c *okapi.Context) error {
	start := time.Now()
	var it models.Integration
	if err := h.DB.First(&it, "id = ? AND organization_id = ?", c.Param("id"), middlewares.OrgID(c)).Error; err != nil {
		return c.AbortNotFound("integration not found")
	}
	var who string
	var err error
	switch it.Kind {
	case models.KindMiabi:
		who, err = h.Miabi.Test(c.Request().Context(), &it)
	case models.KindPosta:
		who, err = h.Mail.Test(c.Request().Context(), &it)
	default:
		who, err = h.Coder.TestIntegration(c.Request().Context(), middlewares.OrgID(c), c.Param("id"))
	}
	out := TestResult{LatencyMs: time.Since(start).Milliseconds()}
	if err != nil {
		out.Error = err.Error()
	} else {
		out.OK, out.Reply = true, "authenticated as "+who
	}
	h.record(c, "integration.test", "integration", c.Param("id"), map[string]any{"ok": out.OK})
	return ok(c, out)
}

// ---- projects ----

// ProjectRequest creates or updates a project.
type ProjectRequest struct {
	Body struct {
		Name          string   `json:"name"`
		Description   string   `json:"description"`
		IntegrationID string   `json:"integration_id"`
		Owner         string   `json:"owner"`
		Repo          string   `json:"repo"`
		CreateRepo    bool     `json:"create_repo" description:"create the repository on the forge"`
		Private       bool     `json:"private"`
		Template      string   `json:"template" description:"with create_repo: a template id from GET /project-templates; queues the scaffold task"`
		AgentID       *string  `json:"agent_id"`
		Selector      []string `json:"selector"`
		SandboxImage  string   `json:"sandbox_image"`
		Instructions  string   `json:"instructions"`
		TriggerLabel  string   `json:"trigger_label"`
		Autonomy      *int     `json:"autonomy" description:"autonomy of the scaffold task (default 2)"`
	} `json:"body"`
}

func (r *ProjectRequest) input() coder.ProjectInput {
	b := r.Body
	return coder.ProjectInput{Name: b.Name, Description: b.Description, IntegrationID: b.IntegrationID, Owner: strings.TrimSpace(b.Owner),
		Repo: strings.TrimSpace(b.Repo), AgentID: b.AgentID, Selector: b.Selector, SandboxImage: b.SandboxImage, Instructions: b.Instructions,
		TriggerLabel: b.TriggerLabel, CreateRepo: b.CreateRepo, Private: b.Private}
}

// ProjectCreated returns the project and the scaffold task, if one was queued.
type ProjectCreated struct {
	Project *models.Project `json:"project"`
	Task    *models.Task    `json:"task,omitempty"`
}

// ListProjects lists projects.
func (h *Handlers) ListProjects(c *okapi.Context) error {
	var out []models.Project
	h.DB.Where("organization_id = ?", middlewares.OrgID(c)).Order("name").Find(&out)
	return ok(c, out)
}

// GetProject returns a project.
func (h *Handlers) GetProject(c *okapi.Context) error {
	p, err := h.Coder.Project(c.Request().Context(), middlewares.OrgID(c), c.Param("id"))
	if err != nil {
		return c.AbortNotFound("project not found")
	}
	return ok(c, p)
}

// CreateProject connects or creates a repository; with a template it also queues the scaffold task.
func (h *Handlers) CreateProject(c *okapi.Context, req *ProjectRequest) error {
	ctx := c.Request().Context()
	in := req.input()
	var tpl coder.Template
	if req.Body.Template != "" {
		var found bool
		if tpl, found = coder.TemplateByID(req.Body.Template); !found {
			return c.AbortBadRequest("unknown template")
		}
		if in.SandboxImage == "" {
			in.SandboxImage = tpl.SandboxImage
		}
		if in.Instructions == "" {
			in.Instructions = tpl.Instructions
		}
	}
	p, err := h.Coder.CreateProject(ctx, middlewares.OrgID(c), middlewares.UserID(c), in)
	if err != nil {
		return c.AbortBadRequest(err.Error())
	}
	out := ProjectCreated{Project: p}
	if tpl.Goal != "" {
		autonomy := proto.AutonomyL2
		if a := req.Body.Autonomy; a != nil {
			autonomy = proto.Autonomy(*a)
		}
		t, err := h.Tasks.Create(ctx, middlewares.OrgID(c), middlewares.UserID(c), tasks.Input{Title: "Scaffold " + p.Name + " (" + tpl.Name + ")",
			Goal: tpl.Goal, ProjectID: &p.ID, Autonomy: autonomy, TimeoutSec: 3600, MaxTurns: 80, Trigger: "template", TriggerRef: tpl.ID})
		if err != nil {
			return c.AbortBadRequest("project created, but the scaffold task failed: " + err.Error())
		}
		out.Task = t
	}
	return created(c, out)
}

// UpdateProject edits a project.
func (h *Handlers) UpdateProject(c *okapi.Context, req *ProjectRequest) error {
	p, err := h.Coder.UpdateProject(c.Request().Context(), middlewares.OrgID(c), middlewares.UserID(c), c.Param("id"), req.input())
	if err != nil {
		return mapErr(c, err)
	}
	return ok(c, p)
}

// DeleteProject removes a project (the repository is left untouched).
func (h *Handlers) DeleteProject(c *okapi.Context) error {
	res := h.DB.Where("id = ? AND organization_id = ?", c.Param("id"), middlewares.OrgID(c)).Delete(&models.Project{})
	if res.RowsAffected == 0 {
		return c.AbortNotFound("project not found")
	}
	h.record(c, "project.delete", "project", c.Param("id"), nil)
	return message(c, "deleted")
}

// ProjectTemplates lists templates and maintenance presets.
type ProjectTemplates struct {
	Templates []coder.Template `json:"templates"`
	Presets   []coder.Preset   `json:"presets"`
}

// ListProjectTemplates returns built-in templates and maintenance presets.
func (h *Handlers) ListProjectTemplates(c *okapi.Context) error {
	return ok(c, ProjectTemplates{Templates: coder.Templates, Presets: coder.Presets})
}

// PresetRequest schedules a maintenance preset on a project.
type PresetRequest struct {
	Body struct {
		Preset   string `json:"preset" required:"true"`
		Cron     string `json:"cron"`
		Autonomy *int   `json:"autonomy" description:"0-3 (default 2)"`
		Enabled  *bool  `json:"enabled"`
	} `json:"body"`
}

// AddMaintenance creates a schedule from a maintenance preset.
func (h *Handlers) AddMaintenance(c *okapi.Context, req *PresetRequest) error {
	p, err := h.Coder.Project(c.Request().Context(), middlewares.OrgID(c), c.Param("id"))
	if err != nil {
		return c.AbortNotFound("project not found")
	}
	var preset *coder.Preset
	for i := range coder.Presets {
		if coder.Presets[i].ID == req.Body.Preset {
			preset = &coder.Presets[i]
		}
	}
	if preset == nil {
		return c.AbortBadRequest("unknown preset")
	}
	cronExpr := preset.Cron
	if req.Body.Cron != "" {
		cronExpr = req.Body.Cron
	}
	autonomy := proto.AutonomyL2
	if req.Body.Autonomy != nil {
		autonomy = proto.Autonomy(*req.Body.Autonomy)
		if !autonomy.Valid() {
			return c.AbortBadRequest("autonomy must be 0-3")
		}
	}
	enabled := req.Body.Enabled == nil || *req.Body.Enabled
	sreq := &ScheduleRequest{}
	sreq.Body.Name, sreq.Body.Cron, sreq.Body.Enabled = p.Name+": "+preset.Name, cronExpr, enabled
	sreq.Body.Template = models.TaskTemplate{Title: preset.Name + " — " + p.Name, Goal: preset.Goal, ProjectID: &p.ID, Autonomy: autonomy,
		MaxTurns: 80, TimeoutSec: 3600, MaxAttempts: 1}
	return h.CreateSchedule(c, sreq)
}

// TaskDiff returns the pull request diff of a coding task.
func (h *Handlers) TaskDiff(c *okapi.Context) error {
	t, err := h.Tasks.Get(c.Request().Context(), middlewares.OrgID(c), c.Param("id"))
	if err != nil {
		return mapErr(c, err)
	}
	diff, err := h.Coder.TaskDiff(c.Request().Context(), t)
	if err != nil {
		return c.AbortBadRequest(err.Error())
	}
	return c.Data(http.StatusOK, "text/x-diff; charset=utf-8", []byte(diff))
}

// ForgeWebhook receives signed issue events and turns labelled issues into tasks.
func (h *Handlers) ForgeWebhook(c *okapi.Context) error {
	ctx := c.Request().Context()
	body, err := io.ReadAll(io.LimitReader(c.Request().Body, 5<<20))
	if err != nil {
		return c.AbortBadRequest("unreadable body")
	}
	trig, err := h.Coder.ParseIssueWebhook(ctx, c.Param("id"), c.Request().Header, body)
	switch {
	case errors.Is(err, coder.ErrIgnored):
		return message(c, "ignored")
	case errors.Is(err, coder.ErrBadSignature), errors.Is(err, coder.ErrNotFound):
		return c.AbortUnauthorized("invalid webhook")
	case err != nil:
		return c.AbortBadRequest(err.Error())
	}
	var n int64
	h.DB.Model(&models.Task{}).Where("trigger_ref = ? AND status NOT IN ?", trig.Ref,
		[]string{models.TaskFailed, models.TaskCancelled, models.TaskTimedOut}).Count(&n)
	if n > 0 {
		return message(c, "a task for this issue already exists")
	}
	t, err := h.Tasks.Create(ctx, trig.Project.OrganizationID, "", tasks.Input{Title: "Issue #" + strconv.Itoa(trig.Number) + ": " + trig.Title,
		Goal: coder.IssueGoal(trig), ProjectID: &trig.Project.ID, Autonomy: proto.AutonomyL2, TimeoutSec: 3600, MaxTurns: 80,
		Trigger: "issue", TriggerRef: trig.Ref})
	if err != nil {
		logger.Warn("issue webhook could not create a task", "ref", trig.Ref, "error", err)
		return c.AbortBadRequest(err.Error())
	}
	return created(c, t)
}
