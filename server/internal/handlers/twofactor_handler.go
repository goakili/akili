// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package handlers

import (
	"errors"
	"time"

	"github.com/goakili/akili/server/internal/auth"
	"github.com/goakili/akili/server/internal/middlewares"
	"github.com/goakili/akili/server/internal/models"
	"github.com/jkaninda/okapi"
)

// TwoFactorStatus describes the caller's second factor.
type TwoFactorStatus struct {
	Enabled           bool `json:"enabled"`
	RecoveryCodesLeft int  `json:"recovery_codes_left"`
}

// TwoFactorSetupRequest starts enrolling an authenticator app.
type TwoFactorSetupRequest struct {
	Body struct {
		Password string `json:"password" required:"true"`
	} `json:"body"`
}

// TwoFactorCodeRequest carries a TOTP code, or a recovery code where one is accepted.
type TwoFactorCodeRequest struct {
	Body struct {
		Code string `json:"code" required:"true"`
	} `json:"body"`
}

// RecoveryCodes are shown once; only their hashes are stored.
type RecoveryCodes struct {
	RecoveryCodes []string `json:"recovery_codes"`
}

// twoFactorErr maps errors for signed-in callers: a wrong code is a 400, since a 401 would end the session.
func (h *Handlers) twoFactorErr(c *okapi.Context, err error) error {
	switch {
	case errors.Is(err, auth.ErrMFALocked):
		return c.AbortTooManyRequests(err.Error())
	case errors.Is(err, auth.ErrInvalidCode):
		return c.AbortBadRequest(err.Error())
	case errors.Is(err, auth.ErrTOTPEnabled), errors.Is(err, auth.ErrTOTPNotEnabled), errors.Is(err, auth.ErrTOTPNoSetup):
		return c.AbortConflict(err.Error())
	}
	return c.AbortInternalServerError("two-factor update failed", err)
}

func (h *Handlers) currentUser(c *okapi.Context) (*models.User, error) {
	return h.Auth.User(c.Request().Context(), middlewares.OrgID(c), middlewares.UserID(c))
}

// GetTwoFactor returns whether the caller has two-factor authentication.
func (h *Handlers) GetTwoFactor(c *okapi.Context) error {
	u, err := h.currentUser(c)
	if err != nil {
		return mapErr(c, err)
	}
	return ok(c, TwoFactorStatus{Enabled: u.TOTPEnabled, RecoveryCodesLeft: len(u.RecoveryCodes)})
}

// SetupTwoFactor creates a pending authenticator secret after re-checking the password.
func (h *Handlers) SetupTwoFactor(c *okapi.Context, req *TwoFactorSetupRequest) error {
	ctx := c.Request().Context()
	u, err := h.currentUser(c)
	if err != nil {
		return mapErr(c, err)
	}
	if u.PasswordHash == "" {
		return c.AbortConflict("two-factor authentication protects password sign-in; your identity provider handles it for single sign-on")
	}
	if !auth.RateLimit(ctx, h.Bus.Redis(), "2fa-setup:"+u.ID, 10, 15*time.Minute, true) {
		return c.AbortTooManyRequests("too many attempts; try again later")
	}
	if _, err := h.Auth.CheckPassword(ctx, u.Email, req.Body.Password); err != nil {
		return c.AbortBadRequest("password is incorrect")
	}
	setup, err := h.Auth.BeginTOTP(ctx, u)
	if err != nil {
		return h.twoFactorErr(c, err)
	}
	h.record(c, "user.2fa_setup", "user", u.ID, nil)
	return ok(c, setup)
}

// EnableTwoFactor confirms the pending secret with a code and returns the recovery codes once.
func (h *Handlers) EnableTwoFactor(c *okapi.Context, req *TwoFactorCodeRequest) error {
	u, err := h.currentUser(c)
	if err != nil {
		return mapErr(c, err)
	}
	codes, err := h.Auth.EnableTOTP(c.Request().Context(), u, req.Body.Code)
	if err != nil {
		return h.twoFactorErr(c, err)
	}
	h.record(c, "user.2fa_enable", "user", u.ID, nil)
	return ok(c, RecoveryCodes{RecoveryCodes: codes})
}

// DisableTwoFactor removes the caller's second factor; it needs a current code or a recovery code.
func (h *Handlers) DisableTwoFactor(c *okapi.Context, req *TwoFactorCodeRequest) error {
	ctx := c.Request().Context()
	u, err := h.currentUser(c)
	if err != nil {
		return mapErr(c, err)
	}
	method, err := h.Auth.VerifySecondFactor(ctx, u, req.Body.Code)
	if err != nil {
		return h.twoFactorErr(c, err)
	}
	if err := h.Auth.DisableTOTP(ctx, u); err != nil {
		return h.twoFactorErr(c, err)
	}
	h.record(c, "user.2fa_disable", "user", u.ID, map[string]any{"method": method})
	return message(c, "two-factor authentication disabled")
}

// RegenerateRecoveryCodes replaces the caller's recovery codes; it needs a current code.
func (h *Handlers) RegenerateRecoveryCodes(c *okapi.Context, req *TwoFactorCodeRequest) error {
	ctx := c.Request().Context()
	u, err := h.currentUser(c)
	if err != nil {
		return mapErr(c, err)
	}
	method, err := h.Auth.VerifySecondFactor(ctx, u, req.Body.Code)
	if err != nil {
		return h.twoFactorErr(c, err)
	}
	codes, err := h.Auth.RegenerateRecoveryCodes(ctx, u)
	if err != nil {
		return h.twoFactorErr(c, err)
	}
	h.record(c, "user.2fa_recovery_codes", "user", u.ID, map[string]any{"method": method})
	return ok(c, RecoveryCodes{RecoveryCodes: codes})
}

// ResetUserTwoFactor removes another user's second factor, for someone who lost their device.
func (h *Handlers) ResetUserTwoFactor(c *okapi.Context) error {
	ctx := c.Request().Context()
	u, err := h.Auth.User(ctx, middlewares.OrgID(c), c.Param("id"))
	if err != nil {
		return mapErr(c, err)
	}
	if u.ID == middlewares.UserID(c) {
		return c.AbortBadRequest("turn off your own two-factor authentication in your account settings")
	}
	if models.RoleRank(u.Role) > models.RoleRank(middlewares.Role(c)) {
		return c.AbortForbidden("you cannot modify a user above your role")
	}
	if !u.TOTPEnabled && u.TOTPSecret == "" {
		return c.AbortConflict(auth.ErrTOTPNotEnabled.Error())
	}
	if err := h.Auth.DisableTOTP(ctx, u); err != nil {
		return h.twoFactorErr(c, err)
	}
	h.record(c, "user.2fa_reset", "user", u.ID, map[string]any{"email": u.Email})
	return message(c, "two-factor authentication reset for "+u.Email)
}
