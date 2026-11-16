// Copyright (c) 2026 Jonas Kaninda.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package handlers

import (
	"github.com/goakili/akili/server/internal/autonomy"
	"github.com/goakili/akili/server/internal/middlewares"
	"github.com/goakili/akili/server/internal/models"
	"github.com/jkaninda/okapi"
)

// signedIn is false for API keys: autonomy is granted by a signed-in person, never by automation.
func signedIn(c *okapi.Context) bool { return c.GetString(middlewares.CtxAuthMethod) == "session" }

const errNotAPerson = "autonomy can only be granted by a signed-in person, not an API key"

type GrantRequest struct {
	Body autonomy.GrantInput `json:"body"`
}

func (h *Handlers) ListGrants(c *okapi.Context) error {
	gs, err := h.Autonomy.ListGrants(c.Request().Context(), middlewares.OrgID(c), c.Query("agent_id"), c.Query("active") == "true")
	if err != nil {
		return mapErr(c, err)
	}
	return ok(c, gs)
}

func (h *Handlers) CreateGrant(c *okapi.Context, req *GrantRequest) error {
	if !signedIn(c) {
		return c.AbortForbidden(errNotAPerson)
	}
	g, err := h.Autonomy.CreateGrant(c.Request().Context(), middlewares.OrgID(c), middlewares.UserID(c), req.Body)
	if err != nil {
		return mapErr(c, err)
	}
	return created(c, g)
}

func (h *Handlers) RevokeGrant(c *okapi.Context) error {
	g, err := h.Autonomy.RevokeGrant(c.Request().Context(), middlewares.OrgID(c), c.Param("id"), middlewares.UserID(c))
	if err != nil {
		return mapErr(c, err)
	}
	return ok(c, g)
}

type WindowRequest struct {
	Body autonomy.WindowInput `json:"body"`
}

func (h *Handlers) ListWindows(c *okapi.Context) error {
	ws, err := h.Autonomy.ListWindows(c.Request().Context(), middlewares.OrgID(c))
	if err != nil {
		return mapErr(c, err)
	}
	return ok(c, ws)
}

func (h *Handlers) CreateWindow(c *okapi.Context, req *WindowRequest) error {
	w, err := h.Autonomy.SaveWindow(c.Request().Context(), middlewares.OrgID(c), middlewares.UserID(c), "", req.Body)
	if err != nil {
		return mapErr(c, err)
	}
	return created(c, w)
}

func (h *Handlers) UpdateWindow(c *okapi.Context, req *WindowRequest) error {
	w, err := h.Autonomy.SaveWindow(c.Request().Context(), middlewares.OrgID(c), middlewares.UserID(c), c.Param("id"), req.Body)
	if err != nil {
		return mapErr(c, err)
	}
	return ok(c, w)
}

func (h *Handlers) DeleteWindow(c *okapi.Context) error {
	if err := h.Autonomy.DeleteWindow(c.Request().Context(), middlewares.OrgID(c), middlewares.UserID(c), c.Param("id")); err != nil {
		return mapErr(c, err)
	}
	return message(c, "deleted")
}

type RunbookRequest struct {
	Body autonomy.RunbookInput `json:"body"`
}

func (h *Handlers) ListRunbooks(c *okapi.Context) error {
	rs, err := h.Autonomy.ListRunbooks(c.Request().Context(), middlewares.OrgID(c))
	if err != nil {
		return mapErr(c, err)
	}
	return ok(c, rs)
}

func (h *Handlers) GetRunbook(c *okapi.Context) error {
	rb, err := h.Autonomy.GetRunbook(c.Request().Context(), middlewares.OrgID(c), c.Param("id"))
	if err != nil {
		return mapErr(c, err)
	}
	return ok(c, rb)
}

func (h *Handlers) CreateRunbook(c *okapi.Context, req *RunbookRequest) error {
	rb, err := h.Autonomy.SaveRunbook(c.Request().Context(), middlewares.OrgID(c), middlewares.UserID(c), "", req.Body)
	if err != nil {
		return mapErr(c, err)
	}
	return created(c, rb)
}

func (h *Handlers) UpdateRunbook(c *okapi.Context, req *RunbookRequest) error {
	rb, err := h.Autonomy.SaveRunbook(c.Request().Context(), middlewares.OrgID(c), middlewares.UserID(c), c.Param("id"), req.Body)
	if err != nil {
		return mapErr(c, err)
	}
	return ok(c, rb)
}

func (h *Handlers) DeleteRunbook(c *okapi.Context) error {
	if err := h.Autonomy.DeleteRunbook(c.Request().Context(), middlewares.OrgID(c), middlewares.UserID(c), c.Param("id")); err != nil {
		return mapErr(c, err)
	}
	return message(c, "deleted")
}

func (h *Handlers) RunbookDraft(c *okapi.Context) error {
	d, err := h.Autonomy.DraftFromChange(c.Request().Context(), middlewares.OrgID(c), c.Param("id"))
	if err != nil {
		return mapErr(c, err)
	}
	return ok(c, d)
}

// AgentAutonomy is what an agent runs on its own and what the evidence suggests.
type AgentAutonomy struct {
	Grants      []models.ApprovalGrant `json:"grants"`
	Suggestions []autonomy.Suggestion  `json:"suggestions"`
}

func (h *Handlers) GetAgentAutonomy(c *okapi.Context) error {
	ctx, org, id := c.Request().Context(), middlewares.OrgID(c), c.Param("id")
	if _, err := h.Fleet.Get(ctx, org, id); err != nil {
		return mapErr(c, err)
	}
	gs, err := h.Autonomy.ListGrants(ctx, org, id, true)
	if err != nil {
		return mapErr(c, err)
	}
	ss, err := h.Autonomy.Suggestions(ctx, org, id)
	if err != nil {
		return mapErr(c, err)
	}
	return ok(c, AgentAutonomy{Grants: gs, Suggestions: ss})
}

type AcceptSuggestionRequest struct {
	Body struct {
		ExpiresInHours int    `json:"expires_in_hours" required:"true"`
		MaxUses        int    `json:"max_uses"`
		Reason         string `json:"reason"`
	} `json:"body"`
}

func (h *Handlers) AcceptSuggestion(c *okapi.Context, req *AcceptSuggestionRequest) error {
	if !signedIn(c) {
		return c.AbortForbidden(errNotAPerson)
	}
	g, err := h.Autonomy.AcceptSuggestion(c.Request().Context(), middlewares.OrgID(c), middlewares.UserID(c), c.Param("id"), c.Param("key"),
		req.Body.ExpiresInHours, req.Body.MaxUses, req.Body.Reason)
	if err != nil {
		return mapErr(c, err)
	}
	return created(c, g)
}

func (h *Handlers) DismissSuggestion(c *okapi.Context) error {
	if err := h.Autonomy.DismissSuggestion(c.Request().Context(), middlewares.OrgID(c), middlewares.UserID(c), c.Param("id"), c.Param("key")); err != nil {
		return mapErr(c, err)
	}
	return message(c, "dismissed")
}

type AutonomySettingsRequest struct {
	Body models.AutonomySettings `json:"body"`
}

func (h *Handlers) GetAutonomySettings(c *okapi.Context) error {
	return ok(c, h.Autonomy.Settings(c.Request().Context(), middlewares.OrgID(c)))
}

func (h *Handlers) SetAutonomySettings(c *okapi.Context, req *AutonomySettingsRequest) error {
	s, err := h.Autonomy.SetSettings(c.Request().Context(), middlewares.OrgID(c), middlewares.UserID(c), req.Body)
	if err != nil {
		return mapErr(c, err)
	}
	return ok(c, s)
}
