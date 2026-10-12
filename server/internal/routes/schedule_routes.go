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
