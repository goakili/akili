// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package handlers

import (
	"errors"
	"time"

	"github.com/goakili/akili/server/internal/audit"
	"github.com/goakili/akili/server/internal/auth"
	"github.com/goakili/akili/server/internal/coder"
	"github.com/goakili/akili/server/internal/middlewares"
	"github.com/goakili/akili/server/internal/models"
	"github.com/jkaninda/okapi"
)

// VSCodeAuthorizeRequest is the user's approval of an editor sign-in, sent from the web UI.
type VSCodeAuthorizeRequest struct {
	Body struct {
		Challenge string `json:"challenge" required:"true" description:"S256 PKCE challenge"`
		State     string `json:"state" required:"true"`
		Client    string `json:"client" required:"true" description:"how the editor describes itself, e.g. VS Code on laptop"`
		Editor    string `json:"editor" required:"true" description:"the editor's URI scheme: vscode, vscode-insiders, vscodium, cursor or windsurf"`
		Window    string `json:"window" description:"the editor window to return to (VS Code's windowId)"`
	} `json:"body"`
}

// VSCodeAuthorized is where the browser sends the code.
type VSCodeAuthorized struct {
	Redirect string `json:"redirect"`
}

// VSCodeAuthorize issues a one-time code for the signed-in user.
func (h *Handlers) VSCodeAuthorize(c *okapi.Context, req *VSCodeAuthorizeRequest) error {
	// Only a browser session may approve: an API key minting longer-lived keys for itself would
	// outlive its own revocation.
	if c.GetString(middlewares.CtxAuthMethod) != "session" {
		return c.AbortForbidden("sign in to Akili in the browser to approve an editor")
	}
	ctx := c.Request().Context()
	if _, err := auth.VSCodeRedirect(req.Body.Editor, req.Body.Window, "akc_check", req.Body.State); err != nil {
		return c.AbortBadRequest(err.Error())
	}
	u, err := h.Auth.User(ctx, middlewares.OrgID(c), middlewares.UserID(c))
	if err != nil {
		return mapErr(c, err)
	}
	code, err := h.Auth.IssueVSCodeCode(ctx, u, req.Body.Challenge, req.Body.Client)
	if err != nil {
		return c.AbortBadRequest(err.Error())
	}
	redirect, _ := auth.VSCodeRedirect(req.Body.Editor, req.Body.Window, code, req.Body.State)
	h.record(c, "vscode.authorize", "user", u.ID, map[string]any{"client": req.Body.Client})
	return ok(c, VSCodeAuthorized{Redirect: redirect})
}

// VSCodeTokenRequest redeems a code.
type VSCodeTokenRequest struct {
	Body struct {
		Code     string `json:"code" required:"true"`
		Verifier string `json:"verifier" required:"true" description:"PKCE code verifier"`
	} `json:"body"`
}

// VSCodeToken is the editor's API key and who it acts as.
type VSCodeToken struct {
	APIKeyCreated
	User *models.User `json:"user"`
}

// VSCodeToken exchanges a code and its verifier for an API key.
func (h *Handlers) VSCodeToken(c *okapi.Context, req *VSCodeTokenRequest) error {
	ctx := c.Request().Context()
	ip := c.RealIP()
	if auth.FailureBlocked(ctx, h.Bus.Redis(), "vscode:"+ip, 20) {
		return c.AbortTooManyRequests("too many failed sign-in attempts")
	}
	u, client, err := h.Auth.RedeemVSCodeCode(ctx, req.Body.Code, req.Body.Verifier)
	if errors.Is(err, auth.ErrVSCodeCode) {
		auth.RecordFailure(ctx, h.Bus.Redis(), "vscode:"+ip, 10*time.Minute)
		return c.AbortUnauthorized(err.Error())
	}
	if err != nil {
		return c.AbortInternalServerError("sign-in failed", err)
	}
	exp := time.Now().UTC().AddDate(0, 0, auth.VSCodeKeyDays)
	k, secret, err := h.Auth.CreateAPIKey(ctx, u, client, []string{models.ScopeRead, models.ScopeWrite}, &exp)
	if err != nil {
		return c.AbortInternalServerError("sign-in failed", err)
	}
	h.Audit.Best(ctx, audit.Entry{OrganizationID: u.OrganizationID, ActorType: audit.ActorUser, ActorID: u.ID, Action: "api_key.create",
		TargetType: "api_key", TargetID: k.ID, IP: ip, Metadata: map[string]any{"name": k.Name, "scopes": k.Scopes, "source": "vscode"}})
	return created(c, VSCodeToken{APIKeyCreated: APIKeyCreated{Key: k, Secret: secret}, User: u})
}

// ResolveProject finds the project for a git remote (?remote=).
func (h *Handlers) ResolveProject(c *okapi.Context) error {
	remote := c.Query("remote")
	if remote == "" {
		return c.AbortBadRequest("remote is required")
	}
	p, err := h.Coder.ResolveRemote(c.Request().Context(), middlewares.OrgID(c), remote)
	if err != nil {
		if errors.Is(err, coder.ErrNotFound) {
			return c.AbortNotFound("no project for this repository")
		}
		return c.AbortBadRequest(err.Error())
	}
	return ok(c, p)
}
