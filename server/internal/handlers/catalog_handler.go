// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/crypto"
	"github.com/goakili/akili/server/internal/gateway"
	"github.com/goakili/akili/server/internal/llm"
	"github.com/goakili/akili/server/internal/middlewares"
	"github.com/goakili/akili/server/internal/models"
	"github.com/jkaninda/okapi"
	"gorm.io/gorm"
)

// ---- skills --------------------------------------------------------------------------------------

// SkillRequest creates or updates a skill.
type SkillRequest struct {
	Body struct {
		Name        string `json:"name" required:"true" maxLength:"120"`
		Description string `json:"description" maxLength:"500"`
		Content     string `json:"content" required:"true"`
	} `json:"body"`
}

// ListSkills lists skills.
func (h *Handlers) ListSkills(c *okapi.Context) error {
	var out []models.Skill
	h.DB.Where("organization_id = ?", middlewares.OrgID(c)).Order("builtin DESC, name").Find(&out)
	return ok(c, out)
}

// GetSkill returns a skill.
func (h *Handlers) GetSkill(c *okapi.Context) error {
	var s models.Skill
	if err := h.DB.First(&s, "id = ? AND organization_id = ?", c.Param("id"), middlewares.OrgID(c)).Error; err != nil {
		return c.AbortNotFound("skill not found")
	}
	return ok(c, s)
}

// CreateSkill adds a skill.
func (h *Handlers) CreateSkill(c *okapi.Context, req *SkillRequest) error {
	s := models.Skill{Base: models.Base{ID: models.NewID("skl"), OrganizationID: middlewares.OrgID(c)}, Name: strings.TrimSpace(req.Body.Name),
		Description: req.Body.Description, Content: req.Body.Content, Version: 1, Hash: crypto.SHA256Hex([]byte(req.Body.Content))}
	if err := h.DB.Create(&s).Error; err != nil {
		return c.AbortInternalServerError("create failed", err)
	}
	h.record(c, "skill.create", "skill", s.ID, map[string]any{"name": s.Name, "hash": s.Hash})
	return created(c, s)
}

// UpdateSkill edits a skill and bumps its version. Running sessions keep the version they started
// with; new sessions get the new one.
func (h *Handlers) UpdateSkill(c *okapi.Context, req *SkillRequest) error {
	var s models.Skill
	if err := h.DB.First(&s, "id = ? AND organization_id = ?", c.Param("id"), middlewares.OrgID(c)).Error; err != nil {
		return c.AbortNotFound("skill not found")
	}
	if s.Builtin {
		return c.AbortForbidden("built-in runbooks are read-only; create a copy to customise")
	}
	hash := crypto.SHA256Hex([]byte(req.Body.Content))
	if hash != s.Hash {
		s.Version++
	}
	s.Name, s.Description, s.Content, s.Hash = strings.TrimSpace(req.Body.Name), req.Body.Description, req.Body.Content, hash
	if err := h.DB.Save(&s).Error; err != nil {
		return c.AbortInternalServerError("update failed", err)
	}
	h.record(c, "skill.update", "skill", s.ID, map[string]any{"version": s.Version, "hash": s.Hash})
	return ok(c, s)
}

// DeleteSkill removes a skill and its assignments.
func (h *Handlers) DeleteSkill(c *okapi.Context) error {
	var s models.Skill
	if err := h.DB.First(&s, "id = ? AND organization_id = ?", c.Param("id"), middlewares.OrgID(c)).Error; err != nil {
		return c.AbortNotFound("skill not found")
	}
	if s.Builtin {
		return c.AbortForbidden("built-in runbooks cannot be deleted")
	}
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Table("agent_skills").Where("skill_id = ?", s.ID).Delete(map[string]any{}).Error; err != nil {
			return err
		}
		return tx.Delete(&s).Error
	})
	if err != nil {
		return c.AbortInternalServerError("delete failed", err)
	}
	h.record(c, "skill.delete", "skill", s.ID, map[string]any{"name": s.Name})
	return message(c, "deleted")
}

// ---- policies ------------------------------------------------------------------------------------

// PolicyRequest creates or updates a policy.
type PolicyRequest struct {
	Body struct {
		Name        string       `json:"name" required:"true" maxLength:"120"`
		Description string       `json:"description" maxLength:"500"`
		Document    proto.Policy `json:"document"`
	} `json:"body"`
}

// ListPolicies lists policies.
func (h *Handlers) ListPolicies(c *okapi.Context) error {
	var out []models.Policy
	h.DB.Where("organization_id = ?", middlewares.OrgID(c)).Order("builtin DESC, name").Find(&out)
	return ok(c, out)
}

// GetPolicy returns a policy.
func (h *Handlers) GetPolicy(c *okapi.Context) error {
	var p models.Policy
	if err := h.DB.First(&p, "id = ? AND organization_id = ?", c.Param("id"), middlewares.OrgID(c)).Error; err != nil {
		return c.AbortNotFound("policy not found")
	}
	return ok(c, p)
}

// CreatePolicy adds a policy.
func (h *Handlers) CreatePolicy(c *okapi.Context, req *PolicyRequest) error {
	doc := req.Body.Document
	if doc.MaxRisk == 0 {
		return c.AbortBadRequest("document.max_risk is required (low, medium, high or critical)")
	}
	doc.Name, doc.Version = req.Body.Name, 1
	p := models.Policy{Base: models.Base{ID: models.NewID("pol"), OrganizationID: middlewares.OrgID(c)}, Name: req.Body.Name,
		Description: req.Body.Description, Version: 1, Document: doc}
	if err := h.DB.Create(&p).Error; err != nil {
		return c.AbortInternalServerError("create failed", err)
	}
	h.record(c, "policy.create", "policy", p.ID, map[string]any{"name": p.Name, "document": p.Document})
	return created(c, p)
}

// UpdatePolicy replaces a policy document and bumps its version. It applies to the next tool request
// of every agent bound to it, including running sessions.
func (h *Handlers) UpdatePolicy(c *okapi.Context, req *PolicyRequest) error {
	var p models.Policy
	if err := h.DB.First(&p, "id = ? AND organization_id = ?", c.Param("id"), middlewares.OrgID(c)).Error; err != nil {
		return c.AbortNotFound("policy not found")
	}
	if p.Builtin {
		return c.AbortForbidden("built-in templates are read-only; create a copy to customise")
	}
	if req.Body.Document.MaxRisk == 0 {
		return c.AbortBadRequest("document.max_risk is required")
	}
	p.Version++
	p.Name, p.Description, p.Document = req.Body.Name, req.Body.Description, req.Body.Document
	p.Document.Name, p.Document.Version = p.Name, p.Version
	if err := h.DB.Save(&p).Error; err != nil {
		return c.AbortInternalServerError("update failed", err)
	}
	h.record(c, "policy.update", "policy", p.ID, map[string]any{"version": p.Version, "document": p.Document})
	return ok(c, p)
}

// DeletePolicy removes an unused, non-built-in policy.
func (h *Handlers) DeletePolicy(c *okapi.Context) error {
	var p models.Policy
	if err := h.DB.First(&p, "id = ? AND organization_id = ?", c.Param("id"), middlewares.OrgID(c)).Error; err != nil {
		return c.AbortNotFound("policy not found")
	}
	if p.Builtin {
		return c.AbortForbidden("built-in templates cannot be deleted")
	}
	var n int64
	h.DB.Model(&models.Agent{}).Where("policy_id = ?", p.ID).Count(&n)
	if n > 0 {
		return c.AbortConflict("the policy is bound to agents")
	}
	h.DB.Delete(&p)
	h.record(c, "policy.delete", "policy", p.ID, map[string]any{"name": p.Name})
	return message(c, "deleted")
}

// SimulateRequest evaluates a hypothetical tool call.
type SimulateRequest struct {
	Body struct {
		Tool     string          `json:"tool" required:"true"`
		Input    json.RawMessage `json:"input"`
		Autonomy proto.Autonomy  `json:"autonomy"`
		Workdir  string          `json:"workdir"`
	} `json:"body"`
}

// SimulatePolicy answers "would this call be allowed?".
func (h *Handlers) SimulatePolicy(c *okapi.Context, req *SimulateRequest) error {
	var p models.Policy
	if err := h.DB.First(&p, "id = ? AND organization_id = ?", c.Param("id"), middlewares.OrgID(c)).Error; err != nil {
		return c.AbortNotFound("policy not found")
	}
	wd := req.Body.Workdir
	if wd == "" {
		wd = "/var/lib/akili-agent/work"
	}
	return ok(c, proto.Evaluate(p.Document, req.Body.Autonomy, wd, proto.Call{Tool: req.Body.Tool, Input: req.Body.Input}))
}

// ListTools returns the tool catalog.
func (h *Handlers) ListTools(c *okapi.Context) error {
	return ok(c, proto.Catalog())
}

// ---- model providers -----------------------------------------------------------------------------

// ProviderRequest creates or updates a model provider. An empty api_key on update keeps the old key.
type ProviderRequest struct {
	Body struct {
		Name            string  `json:"name" required:"true"`
		Kind            string  `json:"kind" required:"true"`
		BaseURL         string  `json:"base_url"`
		Model           string  `json:"model" required:"true"`
		Effort          string  `json:"effort"`
		MaxTokens       int     `json:"max_tokens"`
		ContextTokens   int     `json:"context_tokens" minimum:"0" description:"the model's context window in tokens; 0 = 200000"`
		APIKey          string  `json:"api_key"`
		IsDefault       bool    `json:"is_default"`
		InputPriceMTok  float64 `json:"input_price_mtok"`
		OutputPriceMTok float64 `json:"output_price_mtok"`
	} `json:"body"`
}

// ListProviders lists providers (keys are never returned).
func (h *Handlers) ListProviders(c *okapi.Context) error {
	var out []models.ModelProvider
	h.DB.Where("organization_id = ?", middlewares.OrgID(c)).Order("is_default DESC, name").Find(&out)
	return ok(c, out)
}

func validProvider(kind, effort string) string {
	switch kind {
	case models.ProviderAnthropic, models.ProviderOpenAI, models.ProviderFake:
	default:
		return "kind must be anthropic, openai or fake"
	}
	switch effort {
	case "", "low", "medium", "high", "xhigh", "max":
	default:
		return "effort must be low, medium, high, xhigh or max"
	}
	return ""
}

func (h *Handlers) saveProvider(c *okapi.Context, p *models.ModelProvider, req *ProviderRequest) error {
	b := req.Body
	if msg := validProvider(b.Kind, b.Effort); msg != "" {
		return c.AbortBadRequest(msg)
	}
	p.Name, p.Kind, p.BaseURL, p.Model, p.Effort, p.MaxTokens = b.Name, b.Kind, strings.TrimRight(b.BaseURL, "/"), b.Model, b.Effort, b.MaxTokens
	p.IsDefault, p.InputPriceMTok, p.OutputPriceMTok, p.ContextTokens = b.IsDefault, b.InputPriceMTok, b.OutputPriceMTok, max(b.ContextTokens, 0)
	if p.MaxTokens <= 0 {
		p.MaxTokens = 32000
	}
	if b.APIKey != "" {
		enc, err := h.Box.Encrypt(b.APIKey)
		if err != nil {
			return c.AbortInternalServerError("encrypt failed", err)
		}
		p.APIKeyEnc = enc
	}
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		if p.IsDefault {
			if err := tx.Model(&models.ModelProvider{}).Where("organization_id = ? AND id <> ?", p.OrganizationID, p.ID).Update("is_default", false).Error; err != nil {
				return err
			}
		}
		return tx.Save(p).Error
	})
	if err != nil {
		return c.AbortInternalServerError("save failed", err)
	}
	p.HasKey = p.APIKeyEnc != ""
	return nil
}

// CreateProvider adds a provider.
func (h *Handlers) CreateProvider(c *okapi.Context, req *ProviderRequest) error {
	p := &models.ModelProvider{Base: models.Base{ID: models.NewID("prv"), OrganizationID: middlewares.OrgID(c)}}
	if err := h.saveProvider(c, p, req); err != nil {
		return err
	}
	h.record(c, "provider.create", "provider", p.ID, map[string]any{"name": p.Name, "kind": p.Kind, "model": p.Model})
	return created(c, p)
}

// ProviderAgent is an agent whose model calls go to a provider.
type ProviderAgent struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	// ViaDefault is set for agents without their own provider that use the organization default.
	ViaDefault bool `json:"via_default"`
}

// ProviderUsage totals a provider's model calls.
type ProviderUsage struct {
	Calls        int64   `json:"calls"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
}

// ProviderDetail is a provider with the agents using it and its last 30 days of usage.
type ProviderDetail struct {
	models.ModelProvider
	Agents   []ProviderAgent `json:"agents"`
	Usage30d ProviderUsage   `json:"usage_30d"`
}

// GetProvider returns one provider (never its key).
func (h *Handlers) GetProvider(c *okapi.Context) error {
	org := middlewares.OrgID(c)
	var p models.ModelProvider
	if err := h.DB.First(&p, "id = ? AND organization_id = ?", c.Param("id"), org).Error; err != nil {
		return c.AbortNotFound("provider not found")
	}
	out := ProviderDetail{ModelProvider: p, Agents: []ProviderAgent{}}
	var agents []models.Agent
	q := h.DB.Select("id", "name", "status", "provider_id").Where("organization_id = ? AND status <> ?", org, models.AgentRevoked)
	if p.IsDefault {
		q = q.Where("provider_id = ? OR provider_id IS NULL", p.ID)
	} else {
		q = q.Where("provider_id = ?", p.ID)
	}
	q.Order("name").Find(&agents)
	for _, a := range agents {
		out.Agents = append(out.Agents, ProviderAgent{ID: a.ID, Name: a.Name, Status: a.Status, ViaDefault: a.ProviderID == nil})
	}
	h.DB.Model(&models.Usage{}).Where("organization_id = ? AND provider_id = ? AND created_at >= ?", org, p.ID, time.Now().UTC().AddDate(0, 0, -30)).
		Select("COUNT(*) AS calls, COALESCE(SUM(input_tokens),0) AS input_tokens, COALESCE(SUM(output_tokens),0) AS output_tokens, COALESCE(SUM(cost_usd),0) AS cost_usd").
		Scan(&out.Usage30d)
	return ok(c, out)
}

// UpdateProvider edits a provider.
func (h *Handlers) UpdateProvider(c *okapi.Context, req *ProviderRequest) error {
	var p models.ModelProvider
	if err := h.DB.First(&p, "id = ? AND organization_id = ?", c.Param("id"), middlewares.OrgID(c)).Error; err != nil {
		return c.AbortNotFound("provider not found")
	}
	if err := h.saveProvider(c, &p, req); err != nil {
		return err
	}
	h.record(c, "provider.update", "provider", p.ID, map[string]any{"model": p.Model, "key_changed": req.Body.APIKey != ""})
	return ok(c, p)
}

// DeleteProvider removes a provider not bound to agents.
func (h *Handlers) DeleteProvider(c *okapi.Context) error {
	var p models.ModelProvider
	if err := h.DB.First(&p, "id = ? AND organization_id = ?", c.Param("id"), middlewares.OrgID(c)).Error; err != nil {
		return c.AbortNotFound("provider not found")
	}
	var n int64
	h.DB.Model(&models.Agent{}).Where("provider_id = ?", p.ID).Count(&n)
	if n > 0 {
		return c.AbortConflict("the provider is bound to agents")
	}
	h.DB.Delete(&p)
	h.record(c, "provider.delete", "provider", p.ID, nil)
	return message(c, "deleted")
}

// TestResult reports a provider probe.
type TestResult struct {
	OK        bool   `json:"ok"`
	Reply     string `json:"reply,omitempty"`
	Error     string `json:"error,omitempty"`
	LatencyMs int64  `json:"latency_ms"`
}

// TestProvider sends a tiny request to check connectivity and credentials.
func (h *Handlers) TestProvider(c *okapi.Context) error {
	var p models.ModelProvider
	if err := h.DB.First(&p, "id = ? AND organization_id = ?", c.Param("id"), middlewares.OrgID(c)).Error; err != nil {
		return c.AbortNotFound("provider not found")
	}
	prov, err := gateway.Build(&p, h.Box)
	if err != nil {
		return ok(c, TestResult{Error: err.Error()})
	}
	ctx, cancel := context.WithTimeout(c.Request().Context(), 60*time.Second)
	defer cancel()
	start := time.Now()
	res, err := prov.Stream(ctx, llm.Request{Model: p.Model, System: "Reply with the single word OK.", MaxTokens: 1024, Effort: "low",
		Messages: []proto.Message{proto.TextMessage(proto.RoleUser, "Say OK.")}}, nil)
	out := TestResult{LatencyMs: time.Since(start).Milliseconds()}
	if err != nil {
		out.Error = err.Error()
	} else {
		out.OK, out.Reply = true, res.Message.Text()
	}
	h.record(c, "provider.test", "provider", p.ID, map[string]any{"ok": out.OK})
	return ok(c, out)
}
