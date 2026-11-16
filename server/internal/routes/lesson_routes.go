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

func (r *Router) lessonRoutes() []okapi.RouteDefinition {
	g := r.group("Lessons", "Agents' memory across sessions: proposed by agents, approved by operators.")
	return []okapi.RouteDefinition{
		{
			Method:      http.MethodGet,
			Path:        "/lessons",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.ListLessons,
			Summary:     "List lessons (?status=proposed|approved|rejected&agent_id=)",
			Response:    &dto.PageResponse[models.Lesson]{},
			Options:     pageDocs(),
		},
		{
			Method:      http.MethodPost,
			Path:        "/lessons",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     okapi.H(r.h.CreateLesson),
			Summary:     "Add an approved lesson",
			Request:     &handlers.LessonRequest{},
			Response:    &dto.Response[models.Lesson]{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/lessons/{id}/approve",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     okapi.H(r.h.ApproveLesson),
			Summary:     "Approve a lesson (optionally edited)",
			Request:     &handlers.LessonDecisionRequest{},
			Response:    &dto.Response[models.Lesson]{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/lessons/{id}/reject",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     okapi.H(r.h.RejectLesson),
			Summary:     "Reject a lesson",
			Request:     &handlers.LessonDecisionRequest{},
			Response:    &dto.Response[models.Lesson]{},
		},
		{
			Method:      http.MethodDelete,
			Path:        "/lessons/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.DeleteLesson,
			Summary:     "Delete a lesson",
		},
	}
}
