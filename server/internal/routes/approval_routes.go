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
			Response:    &dto.Response[[]models.Approval]{},
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
