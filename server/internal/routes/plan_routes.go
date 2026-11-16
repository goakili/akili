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
	"github.com/goakili/akili/server/internal/plans"
	"github.com/jkaninda/okapi"
)

func (r *Router) planRoutes() []okapi.RouteDefinition {
	g := r.group("Plans", "Project plans: work written as phases, linked to tasks.")
	return []okapi.RouteDefinition{
		{
			Method:      http.MethodGet,
			Path:        "/projects/{id}/plans",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.ListPlans,
			Summary:     "List a project's plans with their progress",
			Response:    &dto.Response[[]plans.Summary]{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/projects/{id}/plans",
			Group:       g,
			Middlewares: r.guard(models.RoleOperator),
			Handler:     okapi.H(r.h.CreatePlan),
			Summary:     "Create a plan, optionally with phases",
			Request:     &handlers.PlanRequest{},
			Response:    &dto.Response[plans.Detail]{},
		},
		{
			Method:      http.MethodGet,
			Path:        "/plans/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.GetPlan,
			Summary:     "Get a plan with its phases and linked tasks",
			Response:    &dto.Response[plans.Detail]{},
		},
		{
			Method:      http.MethodPut,
			Path:        "/plans/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleOperator),
			Handler:     okapi.H(r.h.UpdatePlan),
			Summary:     "Change a plan's title, description, status or position",
			Request:     &handlers.PlanUpdateRequest{},
			Response:    &dto.Response[models.ProjectPlan]{},
		},
		{
			Method:      http.MethodDelete,
			Path:        "/plans/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleOperator),
			Handler:     r.h.DeletePlan,
			Summary:     "Delete a plan (refused while an unfinished task is linked)",
		},
		{
			Method:      http.MethodPut,
			Path:        "/plans/{id}/phases",
			Group:       g,
			Middlewares: r.guard(models.RoleOperator),
			Handler:     okapi.H(r.h.ReplacePlanPhases),
			Summary:     "Replace a plan's phases; phases sent with their id keep their status",
			Request:     &handlers.PlanPhasesRequest{},
			Response:    &dto.Response[plans.Detail]{},
		},
		{
			Method:      http.MethodPatch,
			Path:        "/plans/{id}/phases/{phase}",
			Group:       g,
			Middlewares: r.guard(models.RoleOperator),
			Handler:     okapi.H(r.h.UpdatePlanPhase),
			Summary:     "Set a phase's status and note",
			Request:     &handlers.PlanPhaseRequest{},
			Response:    &dto.Response[models.PlanPhase]{},
		},
	}
}
