// Copyright (c) 2026 Jonas Kaninda.
//
// This file is part of Akili.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.
//
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
			Response:    &dto.PageResponse[models.AuditLog]{},
			Options:     pageDocs(),
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
		{
			Method:      http.MethodGet,
			Path:        "/system/sandbox",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.GetSandboxSettings,
			Summary:     "How project sandboxes run",
			Response:    &dto.Response[handlers.SandboxSettings]{},
		},
		{
			Method:      http.MethodPut,
			Path:        "/system/sandbox",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     okapi.H(r.h.SetSandboxSettings),
			Summary:     "Run project sandboxes as root or as an unprivileged user",
			Request:     &handlers.SandboxSettingsRequest{},
			Response:    &dto.Response[handlers.SandboxSettings]{},
		},
	}
}
