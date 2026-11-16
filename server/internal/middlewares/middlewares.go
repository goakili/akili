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

// Package middlewares authenticates operators (session JWT or API key) and enforces roles and API-key scopes.
package middlewares

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/goakili/akili/server/internal/auth"
	"github.com/goakili/akili/server/internal/models"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jkaninda/okapi"
	"gorm.io/gorm"
)

const (
	CtxUserID     = "akili_user_id"
	CtxOrgID      = "akili_org_id"
	CtxRole       = "akili_role"
	CtxAuthMethod = "akili_auth_method"
	CtxScopes     = "akili_scopes"
	CtxJTI        = "akili_jti"
	CtxExp        = "akili_exp"
)

// SessionCookie is HttpOnly (no script access) and SameSite=Strict (CSRF).
const SessionCookie = "akili_session"

type Authenticator struct {
	db   *gorm.DB
	auth *auth.Service
	jwt  okapi.JWTAuth
}

func NewAuthenticator(db *gorm.DB, a *auth.Service) *Authenticator {
	au := &Authenticator{db: db, auth: a}
	au.jwt = okapi.JWTAuth{
		SigningSecret: a.Secret(),
		Audience:      auth.Audience,
		Algorithms:    []string{"HS256"},
		TokenLookup:   "header:Authorization,cookie:" + SessionCookie,
		ContextKey:    "akili_claims",
		OnUnauthorized: func(c *okapi.Context) error {
			return c.AbortUnauthorized("invalid or expired session")
		},
		ValidateClaims: au.validateClaims,
	}
	return au
}

// validateClaims re-reads the user on every request, so a role change or deactivation applies immediately.
func (au *Authenticator) validateClaims(c *okapi.Context, claims jwt.Claims) error {
	mc, ok := claims.(jwt.MapClaims)
	if !ok {
		return errors.New("invalid claims")
	}
	sub, _ := mc["sub"].(string)
	jti, _ := mc["jti"].(string)
	if sub == "" || jti == "" {
		return errors.New("invalid token")
	}
	if au.auth.IsRevoked(c.Request().Context(), jti) {
		return errors.New("session has been revoked")
	}
	var u models.User
	if err := au.db.WithContext(c.Request().Context()).First(&u, "id = ?", sub).Error; err != nil || !u.Active {
		return errors.New("account is disabled")
	}
	c.Set(CtxUserID, u.ID)
	c.Set(CtxOrgID, u.OrganizationID)
	c.Set(CtxRole, u.Role)
	c.Set(CtxAuthMethod, "session")
	c.Set(CtxJTI, jti)
	if exp, err := mc.GetExpirationTime(); err == nil && exp != nil {
		c.Set(CtxExp, exp.Time)
	}
	return nil
}

// Authenticate accepts an API key (ak_...) or a session JWT (header or cookie).
func (au *Authenticator) Authenticate(c *okapi.Context) error {
	raw := strings.TrimSpace(strings.TrimPrefix(c.Header("Authorization"), "Bearer "))
	if strings.HasPrefix(raw, auth.APIKeyPrefix) {
		key, u, err := au.auth.VerifyAPIKey(c.Request().Context(), raw)
		if err != nil {
			return c.AbortUnauthorized("invalid API key")
		}
		c.Set(CtxUserID, u.ID)
		c.Set(CtxOrgID, u.OrganizationID)
		c.Set(CtxRole, u.Role)
		c.Set(CtxAuthMethod, "api_key")
		c.Set(CtxScopes, strings.Join(key.Scopes, ","))
		// Reads need "read", anything else "write"; a WebSocket upgrade is a GET but opens a live channel, so it needs "write".
		need := models.ScopeWrite
		if m := c.Request().Method; (m == http.MethodGet || m == http.MethodHead) && !isWebSocketUpgrade(c.Request()) {
			need = models.ScopeRead
		}
		if !slices.Contains(key.Scopes, need) && !(need == models.ScopeRead && slices.Contains(key.Scopes, models.ScopeWrite)) {
			return c.AbortForbidden("API key lacks the " + need + " scope")
		}
		return c.Next()
	}
	return au.jwt.Middleware(c)
}

func isWebSocketUpgrade(r *http.Request) bool {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}
	for _, f := range strings.Split(r.Header.Get("Connection"), ",") {
		if strings.EqualFold(strings.TrimSpace(f), "upgrade") {
			return true
		}
	}
	return false
}

func RequireRole(min string) okapi.Middleware {
	return func(c *okapi.Context) error {
		if models.RoleRank(c.GetString(CtxRole)) < models.RoleRank(min) {
			return c.AbortForbidden("requires the " + min + " role")
		}
		return c.Next()
	}
}

func UserID(c *okapi.Context) string { return c.GetString(CtxUserID) }

func OrgID(c *okapi.Context) string { return c.GetString(CtxOrgID) }

func Role(c *okapi.Context) string { return c.GetString(CtxRole) }

// SessionToken returns the current session's jti and expiry (session auth only).
func SessionToken(c *okapi.Context) (string, time.Time) {
	exp, _ := c.GetTime(CtxExp)
	return c.GetString(CtxJTI), exp
}

func SetSessionCookie(c *okapi.Context, token string, maxAge int, secure bool) {
	http.SetCookie(c.ResponseWriter(), &http.Cookie{Name: SessionCookie, Value: token, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode})
}
