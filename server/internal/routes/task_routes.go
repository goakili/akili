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

func (r *Router) taskRoutes() []okapi.RouteDefinition {
	g := r.group("Tasks", "Autonomous work assigned to agents.")
	return []okapi.RouteDefinition{
		{
			Method:      http.MethodGet,
			Path:        "/tasks",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.ListTasks,
			Summary:     "List tasks",
			Response:    &dto.Response[[]models.Task]{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/tasks",
			Group:       g,
			Middlewares: r.guard(models.RoleOperator),
			Handler:     okapi.H(r.h.CreateTask),
			Summary:     "Queue a task",
			Request:     &handlers.TaskRequest{},
		},
		{
			Method:      http.MethodGet,
			Path:        "/tasks/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.GetTask,
			Summary:     "Get a task",
			Response:    &dto.Response[models.Task]{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/tasks/{id}/cancel",
			Group:       g,
			Middlewares: r.guard(models.RoleOperator),
			Handler:     r.h.CancelTask,
			Summary:     "Cancel a task",
		},
		{
			Method:      http.MethodPost,
			Path:        "/tasks/{id}/start",
			Group:       g,
			Middlewares: r.guard(models.RoleOperator),
			Handler:     r.h.StartTask,
			Summary:     "Queue a draft task",
			Response:    &dto.Response[models.Task]{},
		},
		{
			Method:      http.MethodGet,
			Path:        "/tasks/{id}/plans",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.TaskPlans,
			Summary:     "Plans linked to a task, as the agent received them",
			Response:    &dto.Response[[]models.TaskPlan]{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/tasks/{id}/retry",
			Group:       g,
			Middlewares: r.guard(models.RoleOperator),
			Handler:     r.h.RetryTask,
			Summary:     "Queue a finished task again",
		},
	}
}
