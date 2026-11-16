// Copyright (c) 2026 Jonas Kaninda.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package handlers

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/goakili/akili/server/internal/audit"
	"github.com/goakili/akili/server/internal/auth"
	"github.com/goakili/akili/server/internal/crypto"
	"github.com/goakili/akili/server/internal/middlewares"
	"github.com/goakili/akili/server/internal/models"
	"github.com/goakili/akili/server/internal/siem"
	"github.com/jkaninda/logger"
	"github.com/jkaninda/okapi"
)

const oidcStateCookie = "akili_oidc_state"

type AuthProviders struct {
	Password     bool   `json:"password"`
	PasswordNote string `json:"password_note,omitempty"`
	SSO          bool   `json:"sso"`
	SSOName      string `json:"sso_name,omitempty"`
	SSOLoginURL  string `json:"sso_login_url,omitempty"`
}

func (h *Handlers) Providers(c *okapi.Context) error {
	out := AuthProviders{Password: true}
	if h.OIDC != nil {
		out.SSO, out.SSOName, out.SSOLoginURL = true, h.OIDC.Name(), "/api/v1/auth/oidc/login"
		if h.Cfg.OIDC.DisablePassword {
			out.PasswordNote = "Password sign-in is reserved for the owner (break-glass)."
		}
	}
	return ok(c, out)
}

func (h *Handlers) OIDCLogin(c *okapi.Context) error {
	if h.OIDC == nil {
		return c.AbortNotFound("single sign-on is not configured")
	}
	ctx := c.Request().Context()
	if !auth.RateLimit(ctx, h.Bus.Redis(), "sso:"+c.RealIP(), 30, time.Minute, true) {
		return c.AbortTooManyRequests("too many sign-in attempts")
	}
	target, state, err := h.OIDC.Begin(ctx)
	if err != nil {
		logger.Error("sso login failed", "error", err)
		return c.AbortServiceUnavailable("the identity provider is unavailable")
	}
	// Lax so the provider's top-level redirect carries it; binding the state to this browser stops login CSRF.
	http.SetCookie(c.ResponseWriter(), &http.Cookie{Name: oidcStateCookie, Value: state, Path: "/api/v1/auth/oidc", MaxAge: 600,
		HttpOnly: true, Secure: h.Cfg.CookieSecure, SameSite: http.SameSiteLaxMode})
	http.Redirect(c.ResponseWriter(), c.Request(), target, http.StatusFound)
	return nil
}

func (h *Handlers) OIDCCallback(c *okapi.Context) error {
	if h.OIDC == nil {
		return c.AbortNotFound("single sign-on is not configured")
	}
	ctx := c.Request().Context()
	http.SetCookie(c.ResponseWriter(), &http.Cookie{Name: oidcStateCookie, Value: "", Path: "/api/v1/auth/oidc", MaxAge: -1,
		HttpOnly: true, Secure: h.Cfg.CookieSecure, SameSite: http.SameSiteLaxMode})
	fail := func(reason string) error {
		var org models.Organization
		if h.DB.Order("created_at").First(&org).Error == nil {
			h.Audit.Best(ctx, audit.Entry{OrganizationID: org.ID, ActorType: audit.ActorSystem, Action: "auth.sso_denied", IP: c.RealIP(),
				Metadata: map[string]any{"reason": reason}})
		}
		http.Redirect(c.ResponseWriter(), c.Request(), "/login?sso_error="+url.QueryEscape(reason), http.StatusFound)
		return nil
	}
	q := c.Request().URL.Query()
	if e := q.Get("error"); e != "" {
		return fail("the identity provider returned " + e)
	}
	cookie, err := c.Request().Cookie(oidcStateCookie)
	if err != nil || cookie.Value == "" || cookie.Value != q.Get("state") {
		return fail("the sign-in state does not match this browser; start again")
	}
	u, token, exp, err := h.OIDC.Finish(ctx, q.Get("state"), q.Get("code"))
	if err != nil {
		logger.Warn("sso sign-in refused", "error", err)
		if errors.Is(err, auth.ErrSSODenied) {
			return fail(err.Error())
		}
		return fail("sign-in failed")
	}
	middlewares.SetSessionCookie(c, token, int(time.Until(exp).Seconds()), h.Cfg.CookieSecure)
	h.Audit.Best(ctx, audit.Entry{OrganizationID: u.OrganizationID, ActorType: audit.ActorUser, ActorID: u.ID, Action: "auth.sso_login", IP: c.RealIP(),
		Metadata: map[string]any{"role": u.Role}})
	http.Redirect(c.ResponseWriter(), c.Request(), "/", http.StatusFound)
	return nil
}

// SecurityStatus reports the hardening features in use (no secrets).
type SecurityStatus struct {
	KMS       string           `json:"kms"`
	DataKeys  []crypto.KeyInfo `json:"data_keys"`
	SIEM      []siem.Status    `json:"siem"`
	SSO       bool             `json:"sso"`
	AgentMTLS string           `json:"agent_mtls"`
	TLS       bool             `json:"tls"`
}

func (h *Handlers) Security(c *okapi.Context) error {
	ctx := c.Request().Context()
	out := SecurityStatus{KMS: h.Box.Keyring().Provider(), SSO: h.OIDC != nil, AgentMTLS: h.Cfg.TLS.AgentMTLS, TLS: h.Cfg.TLS.Enabled(), SIEM: []siem.Status{}}
	keys, err := h.Box.Keyring().Keys(ctx)
	if err != nil {
		return c.AbortInternalServerError("cannot list data keys", err)
	}
	out.DataKeys = keys
	if h.SIEM != nil {
		out.SIEM = h.SIEM.Statuses(ctx)
	}
	return ok(c, out)
}
