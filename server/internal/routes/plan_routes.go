// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

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
