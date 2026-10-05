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
