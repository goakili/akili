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

func (r *Router) policyRoutes() []okapi.RouteDefinition {
	g := r.group("Policies", "Default-deny capability policies.")
	return []okapi.RouteDefinition{
		{
			Method:      http.MethodGet,
			Path:        "/policies",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.ListPolicies,
			Summary:     "List policies",
			Response:    &dto.Response[[]models.Policy]{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/policies",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     okapi.H(r.h.CreatePolicy),
			Summary:     "Create a policy",
			Request:     &handlers.PolicyRequest{},
		},
		{
			Method:      http.MethodGet,
			Path:        "/policies/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.GetPolicy,
			Summary:     "Get a policy",
			Response:    &dto.Response[models.Policy]{},
		},
		{
			Method:      http.MethodPut,
			Path:        "/policies/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     okapi.H(r.h.UpdatePolicy),
			Summary:     "Update a policy",
			Request:     &handlers.PolicyRequest{},
		},
		{
			Method:      http.MethodDelete,
			Path:        "/policies/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.DeletePolicy,
			Summary:     "Delete a policy",
		},
		{
			Method:      http.MethodPost,
			Path:        "/policies/{id}/simulate",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     okapi.H(r.h.SimulatePolicy),
			Summary:     "Would this tool call be allowed?",
			Request:     &handlers.SimulateRequest{},
		},
		{
			Method:      http.MethodGet,
			Path:        "/tools",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.ListTools,
			Summary:     "Tool catalog",
		},
	}
}
