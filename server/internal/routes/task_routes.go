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
			Response:    &dto.PageResponse[models.Task]{},
			Options: append(pageDocs(),
				okapi.DocQueryParam("status", "string", "comma-separated statuses", false),
				okapi.DocQueryParam("project_id", "string", "tasks of one project", false),
				okapi.DocQueryParam("has_pr", "boolean", "only tasks that opened a pull request", false)),
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
		{
			Method:      http.MethodPost,
			Path:        "/tasks/{id}/continue",
			Group:       g,
			Middlewares: r.guard(models.RoleOperator),
			Handler:     okapi.H(r.h.ContinueTask),
			Summary:     "Continue a stopped task from where it stopped",
			Description: "Requeues a failed, timed-out or cancelled task on the agent that ran it. The new run starts from the previous run's conversation.",
			Request:     &handlers.ContinueTaskRequest{},
			Response:    &dto.Response[models.Task]{},
		},
	}
}
