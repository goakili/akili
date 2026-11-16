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

func (r *Router) scheduleRoutes() []okapi.RouteDefinition {
	g := r.group("Schedules", "Recurring tasks.")
	return []okapi.RouteDefinition{
		{
			Method:      http.MethodGet,
			Path:        "/schedules",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.ListSchedules,
			Summary:     "List schedules",
			Response:    &dto.Response[[]models.Schedule]{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/schedules",
			Group:       g,
			Middlewares: r.guard(models.RoleOperator),
			Handler:     okapi.H(r.h.CreateSchedule),
			Summary:     "Create a schedule",
			Request:     &handlers.ScheduleRequest{},
		},
		{
			Method:      http.MethodPut,
			Path:        "/schedules/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleOperator),
			Handler:     okapi.H(r.h.UpdateSchedule),
			Summary:     "Update a schedule",
			Request:     &handlers.ScheduleRequest{},
		},
		{
			Method:      http.MethodDelete,
			Path:        "/schedules/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleOperator),
			Handler:     r.h.DeleteSchedule,
			Summary:     "Delete a schedule",
		},
		{
			Method:      http.MethodPost,
			Path:        "/schedules/{id}/run",
			Group:       g,
			Middlewares: r.guard(models.RoleOperator),
			Handler:     r.h.RunSchedule,
			Summary:     "Run a schedule now",
		},
	}
}
