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
