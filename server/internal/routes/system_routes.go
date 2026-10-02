// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package routes

import (
	"net/http"

	"github.com/goakili/akili/server/internal/dto"
	"github.com/goakili/akili/server/internal/handlers"
	"github.com/goakili/akili/server/internal/models"
	"github.com/jkaninda/okapi"
)

func (r *Router) systemRoutes() []okapi.RouteDefinition {
	g := r.group("System", "Overview, usage, audit and the kill switch.")
	return []okapi.RouteDefinition{
		{
			Method:      http.MethodGet,
			Path:        "/overview",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.GetOverview,
			Summary:     "Dashboard summary",
			Response:    &dto.Response[handlers.Overview]{},
		},
		{
			Method:      http.MethodGet,
			Path:        "/usage",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.GetUsage,
			Summary:     "Spend by agent and model",
			Response:    &dto.Response[[]handlers.UsageRow]{},
		},
		{
			Method:      http.MethodGet,
			Path:        "/audit",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.ListAudit,
			Summary:     "Audit log",
			Response:    &dto.Response[handlers.AuditPage]{},
		},
		{
			Method:      http.MethodGet,
			Path:        "/audit/verify",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.VerifyAudit,
			Summary:     "Verify the audit hash chain",
		},
		{
			Method:      http.MethodGet,
			Path:        "/security",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.Security,
			Summary:     "Security status: KMS data keys, SIEM sink delivery, SSO, mTLS",
			Response:    &dto.Response[handlers.SecurityStatus]{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/system/kill-switch",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     okapi.H(r.h.SetKillSwitch),
			Summary:     "Engage or release the organization kill switch",
			Request:     &handlers.KillSwitchRequest{},
		},
	}
}
