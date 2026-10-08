// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package gateway is the LLM gateway agents call over their tunnel. It owns provider keys, picks
// the model, enforces budgets and the kill switch, and records usage. Agents never see a key and
// cannot choose a model or rewrite tool definitions.
package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/audit"
	"github.com/goakili/akili/server/internal/crypto"
	"github.com/goakili/akili/server/internal/llm"
	"github.com/goakili/akili/server/internal/models"
	"github.com/goakili/akili/server/internal/sessions"
	"github.com/jkaninda/logger"
	"gorm.io/gorm"
)

// Gateway serves model calls.
type Gateway struct {
	db    *gorm.DB
	box   *crypto.Box
	audit *audit.Logger
	hub   *sessions.Hub

	mu        sync.Mutex
	providers map[string]cachedProvider // by provider id
	mounts    []func(agentID string, mux *http.ServeMux)
}

// Mount adds routes to every agent's internal API (e.g. the git proxy).
func (g *Gateway) Mount(fn func(agentID string, mux *http.ServeMux)) { g.mounts = append(g.mounts, fn) }

type cachedProvider struct {
	updated time.Time
	p       llm.Provider
}

// New returns a gateway.
func New(db *gorm.DB, box *crypto.Box, a *audit.Logger, hub *sessions.Hub) *Gateway {
	return &Gateway{db: db, box: box, audit: a, hub: hub, providers: map[string]cachedProvider{}}
}

// Handler serves the internal API for one connected agent. Identity is the tunnel: the handler is
// bound to agentID, and every request is checked against sessions that agent owns.
func (g *Gateway) Handler(agentID string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+proto.InternalLLMPath, func(w http.ResponseWriter, r *http.Request) {
		g.serveLLM(w, r, agentID)
	})
	for _, m := range g.mounts {
		m(agentID, mux)
	}
	return mux
}

func (g *Gateway) serveLLM(w http.ResponseWriter, r *http.Request, agentID string) {
	ctx := r.Context()
	var req proto.LLMRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<20)).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	enc := json.NewEncoder(w)
	flusher, _ := w.(http.Flusher)
	var wmu sync.Mutex
	send := func(ev proto.LLMEvent) {
		wmu.Lock()
		defer wmu.Unlock()
		_ = enc.Encode(ev)
		if flusher != nil {
			flusher.Flush()
		}
	}
	fail := func(code, msg string) { send(proto.LLMEvent{Type: proto.LLMEventError, Code: code, Error: msg}) }

	var s models.ChatSession
	if err := g.db.WithContext(ctx).First(&s, "id = ? AND agent_id = ?", req.SessionID, agentID).Error; err != nil || s.Status != models.SessionOpen {
		fail(proto.CodeSessionClosed, "session is not open for this agent")
		return
	}
	var agent models.Agent
	if err := g.db.WithContext(ctx).First(&agent, "id = ?", agentID).Error; err != nil || agent.Status == models.AgentRevoked {
		fail(proto.CodeSessionClosed, "agent is revoked")
		return
	}
	var org models.Organization
	if g.db.WithContext(ctx).First(&org, "id = ?", s.OrganizationID).Error == nil && org.KillSwitch {
		fail(proto.CodeBudgetExhausted, "the organization kill switch is engaged")
		return
	}
	if err := g.checkBudget(ctx, &agent, &s); err != nil {
		fail(proto.CodeBudgetExhausted, err.Error())
		return
	}
	row, provider, err := g.provider(ctx, &agent)
	if err != nil {
		fail(proto.CodeNoProvider, err.Error())
		return
	}

	// The system prompt and tool definitions come from the control plane, not the agent: the tool list
	// is the session's frozen set, described from the shared catalog.
	lreq := llm.Request{Model: row.Model, System: s.System, Messages: g.hub.ResolveImages(ctx, &s, req.Messages), MaxTokens: row.MaxTokens, Effort: row.Effort}
	if lreq.System == "" {
		lreq.System = req.System
	}
	for _, name := range s.ToolNames {
		if spec, ok := proto.LookupTool(name); ok {
			lreq.Tools = append(lreq.Tools, spec.Def())
		}
	}

	start := time.Now()
	res, err := provider.Stream(ctx, lreq, func(kind, text string) {
		send(proto.LLMEvent{Type: proto.LLMEventDelta, Kind: kind, Text: text})
	})
	if err != nil {
		logger.Warn("model call failed", "agent", agentID, "session", s.ID, "provider", row.Kind, "error", err)
		g.audit.Best(ctx, audit.Entry{OrganizationID: s.OrganizationID, ActorType: audit.ActorAgent, ActorID: agentID,
			Action: "llm.error", TargetType: "session", TargetID: s.ID, Metadata: map[string]any{"model": row.Model, "error": err.Error()}})
		code := proto.CodeProviderError
		switch {
		case llm.ContextTooLong(err):
			code = proto.CodeContextTooLong
		case llm.Rejected(err):
			code = proto.CodeProviderRejected
		}
		fail(code, "model call failed: "+err.Error())
		return
	}
	cost := llm.Cost(res.Usage, llm.PriceFor(row.Model, llm.Price{Input: row.InputPriceMTok, Output: row.OutputPriceMTok}))
	in := res.Usage.InputTokens + res.Usage.CacheReadTokens + res.Usage.CacheWriteTokens
	g.recordUsage(ctx, &s, row, in, res.Usage.OutputTokens, cost)
	g.audit.Best(ctx, audit.Entry{OrganizationID: s.OrganizationID, ActorType: audit.ActorAgent, ActorID: agentID,
		Action: "llm.call", TargetType: "session", TargetID: s.ID, Metadata: map[string]any{
			"model": row.Model, "provider": row.ID, "input_tokens": in, "output_tokens": res.Usage.OutputTokens,
			"cost_usd": cost, "stop_reason": res.StopReason, "duration_ms": time.Since(start).Milliseconds()}})
	msg := res.Message
	send(proto.LLMEvent{Type: proto.LLMEventMessage, Message: &msg, StopReason: res.StopReason,
		Usage: &proto.Usage{InputTokens: in, OutputTokens: res.Usage.OutputTokens}})
}

// ErrBudget is returned when a budget is exhausted.
var ErrBudget = errors.New("budget exhausted")

func (g *Gateway) checkBudget(ctx context.Context, agent *models.Agent, s *models.ChatSession) error {
	if s.TaskID != nil {
		var t models.Task
		if g.db.WithContext(ctx).Select("budget_usd", "cost_usd").First(&t, "id = ?", *s.TaskID).Error == nil &&
			t.BudgetUSD > 0 && t.CostUSD >= t.BudgetUSD {
			return fmt.Errorf("%w: task spent $%.4f of $%.2f", ErrBudget, t.CostUSD, t.BudgetUSD)
		}
	}
	if agent.MonthlyBudget > 0 {
		monthStart := time.Date(time.Now().UTC().Year(), time.Now().UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
		var spent float64
		g.db.WithContext(ctx).Model(&models.Usage{}).Where("agent_id = ? AND created_at >= ?", agent.ID, monthStart).
			Select("COALESCE(SUM(cost_usd),0)").Scan(&spent)
		if spent >= agent.MonthlyBudget {
			return fmt.Errorf("%w: agent spent $%.2f of its $%.2f monthly budget", ErrBudget, spent, agent.MonthlyBudget)
		}
	}
	return nil
}

func (g *Gateway) recordUsage(ctx context.Context, s *models.ChatSession, p *models.ModelProvider, in, out int, cost float64) {
	u := models.Usage{OrganizationID: s.OrganizationID, AgentID: s.AgentID, SessionID: s.ID, TaskID: s.TaskID, ProviderID: p.ID,
		Model: p.Model, InputTokens: in, OutputTokens: out, CostUSD: cost}
	if err := g.db.WithContext(ctx).Create(&u).Error; err != nil {
		logger.Warn("usage not recorded", "error", err)
	}
	if s.TaskID != nil {
		g.db.WithContext(ctx).Model(&models.Task{}).Where("id = ?", *s.TaskID).Update("cost_usd", gorm.Expr("cost_usd + ?", cost))
	}
	g.hub.RecordUsage(ctx, s, in, out, cost)
}

// provider resolves the agent's provider, or the organization default.
func (g *Gateway) provider(ctx context.Context, agent *models.Agent) (*models.ModelProvider, llm.Provider, error) {
	var row models.ModelProvider
	q := g.db.WithContext(ctx).Where("organization_id = ?", agent.OrganizationID)
	var err error
	if agent.ProviderID != nil {
		err = q.First(&row, "id = ?", *agent.ProviderID).Error
	} else {
		err = q.Where("is_default").First(&row).Error
	}
	if err != nil {
		return nil, nil, errors.New("no model provider is configured")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if c, ok := g.providers[row.ID]; ok && c.updated.Equal(row.UpdatedAt) {
		return &row, c.p, nil
	}
	p, err := Build(&row, g.box)
	if err != nil {
		return nil, nil, err
	}
	g.providers[row.ID] = cachedProvider{updated: row.UpdatedAt, p: p}
	return &row, p, nil
}

// Build constructs a provider client from its row.
func Build(row *models.ModelProvider, box *crypto.Box) (llm.Provider, error) {
	key, err := box.Decrypt(row.APIKeyEnc)
	if err != nil {
		return nil, fmt.Errorf("provider key: %w", err)
	}
	switch row.Kind {
	case models.ProviderAnthropic:
		if key == "" {
			return nil, errors.New("anthropic provider has no API key")
		}
		return llm.NewAnthropic(key, row.BaseURL), nil
	case models.ProviderOpenAI:
		return llm.NewOpenAICompatible(key, row.BaseURL), nil
	case models.ProviderFake:
		return &llm.Fake{Delay: 15 * time.Millisecond}, nil
	}
	return nil, fmt.Errorf("unknown provider kind %q", row.Kind)
}
