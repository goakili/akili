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
