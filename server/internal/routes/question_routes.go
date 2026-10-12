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

func (r *Router) questionRoutes() []okapi.RouteDefinition {
	g := r.group("Questions", "Choices an agent asks the user to make during a task (ask_user).")
	return []okapi.RouteDefinition{
		{
			Method:      http.MethodGet,
			Path:        "/questions",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.ListQuestions,
			Summary:     "List questions",
			Response:    &dto.PageResponse[models.Question]{},
			Options: append(pageDocs(),
				okapi.DocQueryParam("status", "string", "pending, answered or expired", false),
				okapi.DocQueryParam("task_id", "string", "questions of one task", false),
				okapi.DocQueryParam("session_id", "string", "questions of one session", false)),
		},
		{
			Method:      http.MethodPost,
			Path:        "/questions/{id}/answer",
			Group:       g,
			Middlewares: r.guard(models.RoleOperator),
			Handler:     okapi.H(r.h.AnswerQuestion),
			Summary:     "Answer a question",
			Description: "Pick one of the agent's options (choice) or answer in your own words (text). The answer is a preference, not an approval: risky tool calls still need their own approval.",
			Request:     &handlers.AnswerRequest{},
			Response:    &dto.Response[models.Question]{},
		},
	}
}
