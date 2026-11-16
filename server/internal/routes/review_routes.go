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

func (r *Router) reviewRoutes() []okapi.RouteDefinition {
	g := r.group("Reviews", "A second agent's read-only, advisory review of a change plan or a task's work.")
	return []okapi.RouteDefinition{
		{
			Method:      http.MethodGet,
			Path:        "/reviews",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.ListReviews,
			Summary:     "List reviews (?target_type=change|task&target_id=)",
			Response:    &dto.Response[[]models.Review]{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/changes/{id}/review",
			Group:       g,
			Middlewares: r.guard(models.RoleOperator),
			Handler:     okapi.H(r.h.RequestChangeReview),
			Summary:     "Ask another agent to review a change plan (advisory; it never approves)",
			Request:     &handlers.ReviewRequest{},
			Response:    &dto.Response[models.Review]{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/tasks/{id}/review",
			Group:       g,
			Middlewares: r.guard(models.RoleOperator),
			Handler:     okapi.H(r.h.RequestTaskReview),
			Summary:     "Ask another agent to review a task's work and pull request diff (advisory)",
			Request:     &handlers.ReviewRequest{},
			Response:    &dto.Response[models.Review]{},
		},
	}
}
