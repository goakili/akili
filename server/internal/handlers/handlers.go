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
	"github.com/goakili/akili/server/internal/autonomy"
	"github.com/goakili/akili/server/internal/bus"
	"github.com/goakili/akili/server/internal/chat"
	"github.com/goakili/akili/server/internal/coder"
	"github.com/goakili/akili/server/internal/config"
	"github.com/goakili/akili/server/internal/crypto"
	"github.com/goakili/akili/server/internal/dto"
	"github.com/goakili/akili/server/internal/fleet"
	"github.com/goakili/akili/server/internal/lessons"
	"github.com/goakili/akili/server/internal/mcp"
	"github.com/goakili/akili/server/internal/miabi"
	"github.com/goakili/akili/server/internal/middlewares"
	"github.com/goakili/akili/server/internal/notify"
	"github.com/goakili/akili/server/internal/plans"
	"github.com/goakili/akili/server/internal/reviews"
	"github.com/goakili/akili/server/internal/sessions"
	"github.com/goakili/akili/server/internal/siem"
	"github.com/goakili/akili/server/internal/storage/pagination"
	"github.com/goakili/akili/server/internal/tasks"
	"github.com/jkaninda/okapi"
	"gorm.io/gorm"
)

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
	Plans    *plans.Service
	MCP      *mcp.Service
	Mail     *notify.Mailer
	Autonomy *autonomy.Service
	Reviews  *reviews.Service
}

func ok[T any](c *okapi.Context, data T) error {
	return c.JSON(http.StatusOK, dto.Response[T]{Success: true, Data: data})
}

// pageParams reads ?page (0-based) and ?size. ?limit is still accepted as the size for older clients.
func pageParams(c *okapi.Context) pagination.Page {
	return pagination.New(queryInt(c, "page", 0), queryInt(c, "size", queryInt(c, "limit", 0)))
}

// paged writes one page of a list, with the pageable block only when more rows follow.
func paged[T any](c *okapi.Context, items []T, total int64, p pagination.Page) error {
	if items == nil {
		items = []T{}
	}
	resp := dto.PageResponse[T]{Success: true, Data: items}
	if p.HasNext(total) {
		resp.Pageable = &dto.Pageable{CurrentPage: p.Number, NextPage: p.Number + 1, Size: p.Size,
			TotalPages: p.TotalPages(total), TotalElements: total}
	}
	return c.JSON(http.StatusOK, resp)
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

func mapErr(c *okapi.Context, err error) error {
	switch {
	case errors.Is(err, fleet.ErrNotFound), errors.Is(err, sessions.ErrNotFound), errors.Is(err, tasks.ErrNotFound), errors.Is(err, coder.ErrNotFound),
		errors.Is(err, auth.ErrNotFound), errors.Is(err, plans.ErrNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		return c.AbortNotFound("not found")
	case errors.Is(err, plans.ErrInUse), errors.Is(err, tasks.ErrNotDraft), errors.Is(err, tasks.ErrNotContinuable):
		return c.AbortConflict(err.Error())
	case errors.Is(err, bus.ErrAgentOffline):
		return c.AbortConflict("the agent is not connected")
	case errors.Is(err, sessions.ErrBadAnswer):
		return c.AbortBadRequest(err.Error())
	case errors.Is(err, sessions.ErrApprovalClosed), errors.Is(err, sessions.ErrQuestionClosed):
		return c.AbortConflict(err.Error())
	case errors.Is(err, context.Canceled):
		return nil
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
