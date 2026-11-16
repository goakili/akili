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
