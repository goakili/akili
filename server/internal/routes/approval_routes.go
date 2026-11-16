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
