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

func (r *Router) agentRoutes() []okapi.RouteDefinition {
	g := r.group("Agents", "Enroll, configure and control agents.")
	return []okapi.RouteDefinition{
		{
			Method:      http.MethodGet,
			Path:        "/agents",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.ListAgents,
			Summary:     "List agents",
			Response:    &dto.Response[[]models.Agent]{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/agents",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     okapi.H(r.h.CreateAgent),
			Summary:     "Create an agent (returns a one-time join token)",
			Request:     &handlers.AgentRequest{},
		},
		{
			Method:      http.MethodGet,
			Path:        "/agents/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.GetAgent,
			Summary:     "Get an agent",
			Response:    &dto.Response[models.Agent]{},
		},
		{
			Method:      http.MethodPatch,
			Path:        "/agents/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     okapi.H(r.h.UpdateAgent),
			Summary:     "Update an agent (policy, autonomy, skills, ...)",
			Request:     &handlers.AgentRequest{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/agents/{id}/reenroll",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.ReenrollAgent,
			Summary:     "Revoke the current key and issue a new join token",
		},
		{
			Method:      http.MethodPost,
			Path:        "/agents/{id}/revoke",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.RevokeAgent,
			Summary:     "Revoke an agent and cut its tunnel",
		},
		{
			Method:      http.MethodPost,
			Path:        "/agents/{id}/drain",
			Group:       g,
			Middlewares: r.guard(models.RoleOperator),
			Handler:     okapi.H(r.h.DrainAgent),
			Summary:     "Stop or resume scheduling tasks on an agent",
			Request:     &handlers.DrainRequest{},
		},
		{
			Method:      http.MethodDelete,
			Path:        "/agents/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.DeleteAgent,
			Summary:     "Revoke and delete an agent",
		},
	}
}
