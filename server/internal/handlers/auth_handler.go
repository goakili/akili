// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package handlers

import (
	"errors"
	"strings"
	"time"

	"github.com/goakili/akili/server/internal/audit"
	"github.com/goakili/akili/server/internal/auth"
	"github.com/goakili/akili/server/internal/middlewares"
	"github.com/goakili/akili/server/internal/models"
	"github.com/goakili/akili/server/internal/notify"
	"github.com/jkaninda/okapi"
)

// LoginRequest is the login body.
type LoginRequest struct {
	Body struct {
		Email    string `json:"email" required:"true"`
		Password string `json:"password" required:"true"`
	} `json:"body"`
}

// LoginResponse returns the session.
type LoginResponse struct {
	User      *models.User `json:"user"`
	Token     string       `json:"token"`
	ExpiresAt time.Time    `json:"expires_at"`
}

// Login authenticates an operator and sets the session cookie.
func (h *Handlers) Login(c *okapi.Context, req *LoginRequest) error {
	ctx := c.Request().Context()
	ip := c.RealIP()
	if !auth.RateLimit(ctx, h.Bus.Redis(), "login:"+ip, 10, time.Minute, true) {
		return c.AbortTooManyRequests("too many login attempts; try again in a minute")
	}
	u, token, exp, err := h.Auth.Login(ctx, req.Body.Email, req.Body.Password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			return c.AbortUnauthorized("invalid email or password")
		}
		return c.AbortInternalServerError("login failed", err)
	}
	middlewares.SetSessionCookie(c, token, int(time.Until(exp).Seconds()), h.Cfg.CookieSecure)
	h.Audit.Best(ctx, audit.Entry{OrganizationID: u.OrganizationID, ActorType: audit.ActorUser, ActorID: u.ID, Action: "auth.login", IP: ip})
	return ok(c, LoginResponse{User: u, Token: token, ExpiresAt: exp})
}

// Logout revokes the current session.
func (h *Handlers) Logout(c *okapi.Context) error {
	jti, exp := middlewares.SessionToken(c)
	h.Auth.Revoke(c.Request().Context(), jti, exp)
	middlewares.SetSessionCookie(c, "", -1, h.Cfg.CookieSecure)
	h.record(c, "auth.logout", "user", middlewares.UserID(c), nil)
	return message(c, "logged out")
}

// MeResponse describes the current operator.
type MeResponse struct {
	User         *models.User         `json:"user"`
	Organization *models.Organization `json:"organization"`
	AuthMethod   string               `json:"auth_method"`
}

// Me returns the current operator.
func (h *Handlers) Me(c *okapi.Context) error {
	u, err := h.Auth.User(c.Request().Context(), middlewares.OrgID(c), middlewares.UserID(c))
	if err != nil {
		return mapErr(c, err)
	}
	var org models.Organization
	h.DB.First(&org, "id = ?", u.OrganizationID)
	return ok(c, MeResponse{User: u, Organization: &org, AuthMethod: c.GetString(middlewares.CtxAuthMethod)})
}

// ChangePasswordRequest changes the caller's password.
type ChangePasswordRequest struct {
	Body struct {
		Current string `json:"current_password" required:"true"`
		New     string `json:"new_password" required:"true"`
	} `json:"body"`
}

// ChangePassword updates the caller's password after checking the current one.
func (h *Handlers) ChangePassword(c *okapi.Context, req *ChangePasswordRequest) error {
	ctx := c.Request().Context()
	u, err := h.Auth.User(ctx, middlewares.OrgID(c), middlewares.UserID(c))
	if err != nil {
		return mapErr(c, err)
	}
	if _, _, _, err := h.Auth.Login(ctx, u.Email, req.Body.Current); err != nil {
		return c.AbortUnauthorized("current password is incorrect")
	}
	hash, err := auth.HashPassword(req.Body.New)
	if err != nil {
		return c.AbortBadRequest(err.Error())
	}
	if err := h.DB.Model(u).Update("password_hash", hash).Error; err != nil {
		return c.AbortInternalServerError("update failed", err)
	}
	h.record(c, "user.password_change", "user", u.ID, nil)
	return message(c, "password changed")
}

// NotificationSettings are the current user's email preferences.
type NotificationSettings struct {
	EmailApprovals bool `json:"email_approvals"`
	EmailTasks     bool `json:"email_tasks"`
	// EmailAvailable is false until an admin adds a default Posta integration.
	EmailAvailable bool `json:"email_available"`
	// CanApprove: approval email only goes to operators and above.
	CanApprove bool `json:"can_approve"`
}

// NotificationsRequest changes email preferences; omitted fields are kept.
type NotificationsRequest struct {
	Body struct {
		EmailApprovals *bool `json:"email_approvals"`
		EmailTasks     *bool `json:"email_tasks"`
	} `json:"body"`
}

func (h *Handlers) notificationSettings(c *okapi.Context, u *models.User) NotificationSettings {
	return NotificationSettings{EmailApprovals: u.EmailApprovals, EmailTasks: u.EmailTasks,
		EmailAvailable: h.Mail.Available(c.Request().Context(), middlewares.OrgID(c)), CanApprove: models.RoleRank(u.Role) >= models.RoleRank(models.RoleOperator)}
}

// GetNotifications returns the current user's email preferences.
func (h *Handlers) GetNotifications(c *okapi.Context) error {
	u, err := h.Auth.User(c.Request().Context(), middlewares.OrgID(c), middlewares.UserID(c))
	if err != nil {
		return mapErr(c, err)
	}
	return ok(c, h.notificationSettings(c, u))
}

// UpdateNotifications changes the current user's email preferences.
func (h *Handlers) UpdateNotifications(c *okapi.Context, req *NotificationsRequest) error {
	u, err := h.Auth.User(c.Request().Context(), middlewares.OrgID(c), middlewares.UserID(c))
	if err != nil {
		return mapErr(c, err)
	}
	set := map[string]any{}
	if req.Body.EmailApprovals != nil {
		set["email_approvals"], u.EmailApprovals = *req.Body.EmailApprovals, *req.Body.EmailApprovals
	}
	if req.Body.EmailTasks != nil {
		set["email_tasks"], u.EmailTasks = *req.Body.EmailTasks, *req.Body.EmailTasks
	}
	if len(set) > 0 {
		if err := h.DB.Model(u).Updates(set).Error; err != nil {
			return c.AbortInternalServerError("update failed", err)
		}
		h.record(c, "user.notifications", "user", u.ID, set)
	}
	return ok(c, h.notificationSettings(c, u))
}

// TestNotification emails the current user now.
func (h *Handlers) TestNotification(c *okapi.Context) error {
	ctx := c.Request().Context()
	u, err := h.Auth.User(ctx, middlewares.OrgID(c), middlewares.UserID(c))
	if err != nil {
		return mapErr(c, err)
	}
	id, err := h.Mail.SendTest(ctx, middlewares.OrgID(c), u)
	if errors.Is(err, notify.ErrNoPosta) {
		return c.AbortConflict(err.Error())
	}
	if err != nil {
		return c.AbortBadGateway("Posta did not accept the email: " + err.Error())
	}
	return message(c, "test email sent to "+u.Email+" (Posta id "+id+")")
}

// ---- users ---------------------------------------------------------------------------------------

// ListUsers lists the organization's users.
func (h *Handlers) ListUsers(c *okapi.Context) error {
	var users []models.User
	if err := h.DB.Where("organization_id = ?", middlewares.OrgID(c)).Order("email").Find(&users).Error; err != nil {
		return c.AbortInternalServerError("list failed", err)
	}
	return ok(c, users)
}

// UserRequest creates or updates a user.
type UserRequest struct {
	Body struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Role     string `json:"role"`
		Password string `json:"password"`
		Active   *bool  `json:"active"`
	} `json:"body"`
}

func validRole(r string) bool { return models.RoleRank(r) > 0 }

// CreateUser adds an operator.
func (h *Handlers) CreateUser(c *okapi.Context, req *UserRequest) error {
	b := req.Body
	if !strings.Contains(b.Email, "@") {
		return c.AbortBadRequest("a valid email is required")
	}
	if !validRole(b.Role) {
		return c.AbortBadRequest("role must be viewer, operator, admin or owner")
	}
	if models.RoleRank(b.Role) > models.RoleRank(middlewares.Role(c)) {
		return c.AbortForbidden("you cannot grant a role above your own")
	}
	hash, err := auth.HashPassword(b.Password)
	if err != nil {
		return c.AbortBadRequest(err.Error())
	}
	u := models.User{Base: models.Base{ID: models.NewID("usr"), OrganizationID: middlewares.OrgID(c)}, Email: strings.ToLower(strings.TrimSpace(b.Email)),
		Name: b.Name, Role: b.Role, PasswordHash: hash, Active: true}
	if err := h.DB.Create(&u).Error; err != nil {
		return c.AbortConflict("a user with this email already exists")
	}
	h.record(c, "user.create", "user", u.ID, map[string]any{"email": u.Email, "role": u.Role})
	return created(c, u)
}

// UpdateUser changes a user's name, role, active flag or password.
func (h *Handlers) UpdateUser(c *okapi.Context, req *UserRequest) error {
	u, err := h.Auth.User(c.Request().Context(), middlewares.OrgID(c), c.Param("id"))
	if err != nil {
		return mapErr(c, err)
	}
	callerRank := models.RoleRank(middlewares.Role(c))
	if models.RoleRank(u.Role) > callerRank {
		return c.AbortForbidden("you cannot modify a user above your role")
	}
	b := req.Body
	updates := map[string]any{}
	if b.Name != "" {
		updates["name"] = b.Name
	}
	if b.Role != "" {
		if !validRole(b.Role) || models.RoleRank(b.Role) > callerRank {
			return c.AbortBadRequest("invalid role")
		}
		if u.ID == middlewares.UserID(c) && b.Role != u.Role {
			return c.AbortBadRequest("you cannot change your own role")
		}
		updates["role"] = b.Role
	}
	if b.Active != nil {
		if u.ID == middlewares.UserID(c) && !*b.Active {
			return c.AbortBadRequest("you cannot deactivate yourself")
		}
		updates["active"] = *b.Active
	}
	if b.Password != "" {
		hash, err := auth.HashPassword(b.Password)
		if err != nil {
			return c.AbortBadRequest(err.Error())
		}
		updates["password_hash"] = hash
	}
	if len(updates) > 0 {
		if err := h.DB.Model(u).Updates(updates).Error; err != nil {
			return c.AbortInternalServerError("update failed", err)
		}
	}
	delete(updates, "password_hash")
	h.record(c, "user.update", "user", u.ID, updates)
	u, _ = h.Auth.User(c.Request().Context(), middlewares.OrgID(c), u.ID)
	return ok(c, u)
}

// ---- API keys ------------------------------------------------------------------------------------

// ListAPIKeys lists the caller's API keys.
func (h *Handlers) ListAPIKeys(c *okapi.Context) error {
	var keys []models.APIKey
	h.DB.Where("user_id = ?", middlewares.UserID(c)).Order("created_at DESC").Find(&keys)
	return ok(c, keys)
}

// APIKeyRequest creates an API key.
type APIKeyRequest struct {
	Body struct {
		Name          string   `json:"name" required:"true"`
		Scopes        []string `json:"scopes"`
		ExpiresInDays int      `json:"expires_in_days"`
	} `json:"body"`
}

// APIKeyCreated returns the secret once.
type APIKeyCreated struct {
	Key    *models.APIKey `json:"key"`
	Secret string         `json:"secret"`
}

// CreateAPIKey issues a key for the caller.
func (h *Handlers) CreateAPIKey(c *okapi.Context, req *APIKeyRequest) error {
	u, err := h.Auth.User(c.Request().Context(), middlewares.OrgID(c), middlewares.UserID(c))
	if err != nil {
		return mapErr(c, err)
	}
	var exp *time.Time
	if req.Body.ExpiresInDays > 0 {
		t := time.Now().UTC().AddDate(0, 0, req.Body.ExpiresInDays)
		exp = &t
	}
	k, secret, err := h.Auth.CreateAPIKey(c.Request().Context(), u, req.Body.Name, req.Body.Scopes, exp)
	if err != nil {
		return c.AbortBadRequest(err.Error())
	}
	h.record(c, "api_key.create", "api_key", k.ID, map[string]any{"name": k.Name, "scopes": k.Scopes})
	return created(c, APIKeyCreated{Key: k, Secret: secret})
}

// RevokeAPIKey revokes one of the caller's keys.
func (h *Handlers) RevokeAPIKey(c *okapi.Context) error {
	res := h.DB.Model(&models.APIKey{}).Where("id = ? AND user_id = ? AND revoked_at IS NULL", c.Param("id"), middlewares.UserID(c)).
		Update("revoked_at", time.Now().UTC())
	if res.RowsAffected == 0 {
		return c.AbortNotFound("API key not found")
	}
	h.record(c, "api_key.revoke", "api_key", c.Param("id"), nil)
	return message(c, "API key revoked")
}
