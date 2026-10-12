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

func (r *Router) approvalRoutes() []okapi.RouteDefinition {
	g := r.group("Approvals", "Human decisions on high-risk tool calls.")
	return []okapi.RouteDefinition{
		{
			Method:      http.MethodGet,
			Path:        "/approvals",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.ListApprovals,
			Summary:     "List approvals",
			Response:    &dto.PageResponse[models.Approval]{},
			Options:     pageDocs(),
		},
		{
			Method:      http.MethodPost,
			Path:        "/approvals/{id}/approve",
			Group:       g,
			Middlewares: r.guard(models.RoleOperator),
			Handler:     okapi.H(r.h.Approve),
			Summary:     "Approve the exact tool call",
			Request:     &handlers.DecisionRequest{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/approvals/{id}/deny",
			Group:       g,
			Middlewares: r.guard(models.RoleOperator),
			Handler:     okapi.H(r.h.Deny),
			Summary:     "Deny the tool call",
			Request:     &handlers.DecisionRequest{},
		},
	}
}
