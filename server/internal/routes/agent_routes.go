// Copyright (c) 2026 Jonas Kaninda.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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
