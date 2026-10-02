// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/auth"
	"github.com/goakili/akili/server/internal/fleet"
	"github.com/goakili/akili/server/internal/middlewares"
	"github.com/gorilla/websocket"
	"github.com/jkaninda/logger"
	"github.com/jkaninda/okapi"
)

// AgentRequest creates or updates an agent. Omitted fields are left unchanged on update.
type AgentRequest struct {
	Body struct {
		Name          *string         `json:"name"`
		Description   *string         `json:"description"`
		Labels        []string        `json:"labels"`
		PolicyID      *string         `json:"policy_id"`
		Autonomy      *proto.Autonomy `json:"autonomy"`
		ProviderID    *string         `json:"provider_id"`
		MaxParallel   *int            `json:"max_parallel"`
		Instructions  *string         `json:"instructions"`
		MonthlyBudget *float64        `json:"monthly_budget_usd"`
		GitName       *string         `json:"git_name" maxLength:"120"`
		GitEmail      *string         `json:"git_email" maxLength:"254"`
		SkillIDs      *[]string       `json:"skill_ids"`
	} `json:"body"`
}

func (r *AgentRequest) input() fleet.Input {
	b := r.Body
	in := fleet.Input{Name: b.Name, Description: b.Description, Labels: b.Labels, PolicyID: b.PolicyID, Autonomy: b.Autonomy,
		ProviderID: b.ProviderID, MaxParallel: b.MaxParallel, Instructions: b.Instructions, MonthlyBudget: b.MonthlyBudget,
		GitName: b.GitName, GitEmail: b.GitEmail}
	if b.SkillIDs != nil {
		in.SetSkills, in.SkillIDs = true, *b.SkillIDs
	}
	return in
}

// ListAgents lists agents.
func (h *Handlers) ListAgents(c *okapi.Context) error {
	agents, err := h.Fleet.List(c.Request().Context(), middlewares.OrgID(c))
	if err != nil {
		return c.AbortInternalServerError("list failed", err)
	}
	return ok(c, agents)
}

// GetAgent returns an agent.
func (h *Handlers) GetAgent(c *okapi.Context) error {
	a, err := h.Fleet.Get(c.Request().Context(), middlewares.OrgID(c), c.Param("id"))
	if err != nil {
		return mapErr(c, err)
	}
	return ok(c, a)
}

// CreateAgent registers an agent and returns its one-time join token.
func (h *Handlers) CreateAgent(c *okapi.Context, req *AgentRequest) error {
	enr, err := h.Fleet.Create(c.Request().Context(), middlewares.OrgID(c), middlewares.UserID(c), req.input())
	if err != nil {
		return mapErr(c, err)
	}
	return created(c, enr)
}

// UpdateAgent edits an agent.
func (h *Handlers) UpdateAgent(c *okapi.Context, req *AgentRequest) error {
	a, err := h.Fleet.Update(c.Request().Context(), middlewares.OrgID(c), middlewares.UserID(c), c.Param("id"), req.input())
	if err != nil {
		return mapErr(c, err)
	}
	return ok(c, a)
}

// ReenrollAgent issues a new join token and revokes the current key.
func (h *Handlers) ReenrollAgent(c *okapi.Context) error {
	enr, err := h.Fleet.Reenroll(c.Request().Context(), middlewares.OrgID(c), middlewares.UserID(c), c.Param("id"))
	if err != nil {
		return mapErr(c, err)
	}
	return ok(c, enr)
}

// RevokeAgent permanently disables an agent.
func (h *Handlers) RevokeAgent(c *okapi.Context) error {
	if err := h.Fleet.Revoke(c.Request().Context(), middlewares.OrgID(c), middlewares.UserID(c), c.Param("id")); err != nil {
		return mapErr(c, err)
	}
	return message(c, "agent revoked")
}

// DrainRequest toggles draining.
type DrainRequest struct {
	Body struct {
		Draining bool `json:"draining"`
	} `json:"body"`
}

// DrainAgent stops or resumes scheduling onto an agent.
func (h *Handlers) DrainAgent(c *okapi.Context, req *DrainRequest) error {
	if err := h.Fleet.SetDraining(c.Request().Context(), middlewares.OrgID(c), middlewares.UserID(c), c.Param("id"), req.Body.Draining); err != nil {
		return mapErr(c, err)
	}
	return message(c, "updated")
}

// DeleteAgent revokes and removes an agent.
func (h *Handlers) DeleteAgent(c *okapi.Context) error {
	if err := h.Fleet.Delete(c.Request().Context(), middlewares.OrgID(c), middlewares.UserID(c), c.Param("id")); err != nil {
		return mapErr(c, err)
	}
	return message(c, "agent deleted")
}

// ---- agent-facing endpoints (no user auth) -------------------------------------------------------

// Enroll exchanges a join token for an agent identity.
func (h *Handlers) Enroll(c *okapi.Context) error {
	ctx := c.Request().Context()
	ip := c.RealIP()
	// Only failures count per IP: each success spends a one-time token an admin created, and a
	// whole fleet may enroll from behind one NAT.
	if auth.FailureBlocked(ctx, h.Bus.Redis(), "enroll:"+ip, 20) {
		return c.AbortTooManyRequests("too many failed enrollment attempts")
	}
	if err := h.checkAgentCert(c); err != nil {
		auth.RecordFailure(ctx, h.Bus.Redis(), "enroll:"+ip, 10*time.Minute)
		return c.AbortUnauthorized(err.Error())
	}
	var req proto.EnrollRequest
	if err := json.NewDecoder(http.MaxBytesReader(c.ResponseWriter(), c.Request().Body, 64<<10)).Decode(&req); err != nil {
		return c.AbortBadRequest("invalid enrollment request")
	}
	resp, err := h.Fleet.Enroll(ctx, req, ip)
	if err != nil {
		if errors.Is(err, fleet.ErrBadToken) {
			auth.RecordFailure(ctx, h.Bus.Redis(), "enroll:"+ip, 10*time.Minute)
			return c.AbortUnauthorized(err.Error())
		}
		return c.AbortBadRequest(err.Error())
	}
	return c.JSON(http.StatusOK, resp)
}

// checkAgentCert enforces AKILI_AGENT_MTLS. The TLS layer already verified any presented
// certificate against the agent CA (VerifyClientCertIfGiven); "required" rejects agents without one.
// Browsers are unaffected because only the agent endpoints check.
func (h *Handlers) checkAgentCert(c *okapi.Context) error {
	if h.Cfg.TLS.AgentMTLS != "required" {
		return nil
	}
	r := c.Request()
	if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 {
		return errors.New("a client certificate signed by the agent CA is required")
	}
	return nil
}

// agentUpgrader refuses browser origins: agents never send an Origin header, so any request that
// does is a browser being used for cross-site WebSocket hijacking.
var agentUpgrader = websocket.Upgrader{
	ReadBufferSize:  32 << 10,
	WriteBufferSize: 32 << 10,
	CheckOrigin:     func(r *http.Request) bool { return r.Header.Get("Origin") == "" },
}

// Connect authenticates an agent's signed handshake and upgrades to the tunnel.
func (h *Handlers) Connect(c *okapi.Context) error {
	ctx := c.Request().Context()
	ip := c.RealIP()
	// Failed handshakes are limited per IP; signed (successful) ones per agent, so a fleet behind one
	// NAT can all reconnect at once after a control-plane restart.
	if auth.FailureBlocked(ctx, h.Bus.Redis(), "connect:"+ip, 60) {
		return c.AbortTooManyRequests("too many failed connection attempts")
	}
	if err := h.checkAgentCert(c); err != nil {
		auth.RecordFailure(ctx, h.Bus.Redis(), "connect:"+ip, 10*time.Minute)
		logger.Warn("agent connection refused", "ip", ip, "agent", c.Header(proto.HeaderAgentID), "reason", err)
		return c.AbortUnauthorized(err.Error())
	}
	agent, err := h.Fleet.AuthenticateConnect(ctx, c.Request().Header, ip)
	if err == nil && !auth.RateLimit(ctx, h.Bus.Redis(), "connect-agent:"+agent.ID, 30, time.Minute, false) {
		return c.AbortTooManyRequests("this agent is reconnecting too often")
	}
	if err != nil {
		auth.RecordFailure(ctx, h.Bus.Redis(), "connect:"+ip, 10*time.Minute)
		logger.Warn("agent connection refused", "ip", ip, "agent", c.Header(proto.HeaderAgentID), "reason", err)
		return c.AbortUnauthorized("agent authentication failed")
	}
	ws, err := agentUpgrader.Upgrade(c.ResponseWriter(), c.Request(), nil)
	if err != nil {
		return nil
	}
	h.Tunnels.Handle(agent, ws, c.Header(proto.HeaderVersion), ip)
	return nil
}
