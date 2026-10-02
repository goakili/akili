// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package handlers holds the thin HTTP handlers: bind, call a service, audit, respond.
package handlers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/goakili/akili/server/internal/audit"
	"github.com/goakili/akili/server/internal/auth"
	"github.com/goakili/akili/server/internal/bus"
	"github.com/goakili/akili/server/internal/chat"
	"github.com/goakili/akili/server/internal/coder"
	"github.com/goakili/akili/server/internal/config"
	"github.com/goakili/akili/server/internal/crypto"
	"github.com/goakili/akili/server/internal/dto"
	"github.com/goakili/akili/server/internal/enterprise"
	"github.com/goakili/akili/server/internal/fleet"
	"github.com/goakili/akili/server/internal/lessons"
	"github.com/goakili/akili/server/internal/mcp"
	"github.com/goakili/akili/server/internal/miabi"
	"github.com/goakili/akili/server/internal/middlewares"
	"github.com/goakili/akili/server/internal/notify"
	"github.com/goakili/akili/server/internal/sessions"
	"github.com/goakili/akili/server/internal/siem"
	"github.com/goakili/akili/server/internal/tasks"
	"github.com/jkaninda/okapi"
	"gorm.io/gorm"
)

// Handlers bundles every service the HTTP layer uses.
type Handlers struct {
	Cfg      *config.Config
	DB       *gorm.DB
	Bus      *bus.Bus
	Audit    *audit.Logger
	Auth     *auth.Service
	Fleet    *fleet.Service
	Tunnels  *fleet.Manager
	Sessions *sessions.Hub
	Tasks    *tasks.Service
	Box      *crypto.Box
	Coder    *coder.Service
	Miabi    *miabi.Service
	OIDC     *auth.OIDC // nil unless SSO is configured
	SIEM     *siem.Forwarder
	Chat     *chat.Service
	Lessons  *lessons.Service
	MCP      *mcp.Service
	Mail     *notify.Mailer
	EE       enterprise.EE
}

func ok[T any](c *okapi.Context, data T) error {
	return c.JSON(http.StatusOK, dto.Response[T]{Success: true, Data: data})
}

func created[T any](c *okapi.Context, data T) error {
	return c.JSON(http.StatusCreated, dto.Response[T]{Success: true, Data: data})
}

func message(c *okapi.Context, msg string) error { return ok(c, dto.Message{Message: msg}) }

func auditEntry(c *okapi.Context, action string, meta map[string]any) audit.Entry {
	return audit.Entry{OrganizationID: middlewares.OrgID(c), ActorType: audit.ActorUser, ActorID: middlewares.UserID(c),
		Action: action, TargetType: "organization", TargetID: middlewares.OrgID(c), IP: c.RealIP(), Metadata: meta}
}

// record audits a user action; best-effort, since the action already happened.
func (h *Handlers) record(c *okapi.Context, action, targetType, targetID string, meta map[string]any) {
	h.Audit.Best(c.Request().Context(), audit.Entry{OrganizationID: middlewares.OrgID(c), ActorType: audit.ActorUser,
		ActorID: middlewares.UserID(c), Action: action, TargetType: targetType, TargetID: targetID, IP: c.RealIP(), Metadata: meta})
}

// mapErr turns service errors into HTTP errors.
func mapErr(c *okapi.Context, err error) error {
	switch {
	case errors.Is(err, fleet.ErrNotFound), errors.Is(err, sessions.ErrNotFound), errors.Is(err, tasks.ErrNotFound), errors.Is(err, coder.ErrNotFound),
		errors.Is(err, auth.ErrNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		return c.AbortNotFound("not found")
	case errors.Is(err, bus.ErrAgentOffline):
		return c.AbortConflict("the agent is not connected")
	case errors.Is(err, sessions.ErrApprovalClosed):
		return c.AbortConflict(err.Error())
	case errors.Is(err, context.Canceled):
		return nil
	case errors.Is(err, enterprise.ErrLicenseRequired), errors.Is(err, enterprise.ErrEntitlementDenied):
		return c.AbortPaymentRequired(err.Error())
	case errors.Is(err, enterprise.ErrLicenseExpired), errors.Is(err, enterprise.ErrBindingMismatch):
		return c.AbortForbidden(err.Error())
	case errors.Is(err, enterprise.ErrCommunityEdition), errors.Is(err, enterprise.ErrNoPublicKey):
		return c.AbortConflict(err.Error())
	}
	return c.AbortBadRequest(err.Error())
}

func queryInt(c *okapi.Context, key string, def int) int {
	if v, err := strconv.Atoi(c.Query(key)); err == nil {
		return v
	}
	return def
}

// pingInterval keeps SSE connections alive through proxies.
const pingInterval = 20 * time.Second

// streamEvents writes bus events to the client as SSE until it disconnects.
func streamEvents(c *okapi.Context, sub *bus.Subscription, filter func(bus.Event) bool) error {
	defer sub.Close()
	ctx := c.Request().Context()
	ping := time.NewTicker(pingInterval)
	defer ping.Stop()
	if err := c.SSESendJSON(bus.Event{Type: "ready", TS: time.Now().UTC()}); err != nil {
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, open := <-sub.C:
			if !open {
				return nil
			}
			if filter != nil && !filter(ev) {
				continue
			}
			if err := c.SSESendJSON(ev); err != nil {
				return nil
			}
		case <-ping.C:
			if err := c.SSESendJSON(bus.Event{Type: "ping", TS: time.Now().UTC()}); err != nil {
				return nil
			}
		}
	}
}
