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

package handlers

import (
	"github.com/goakili/akili/server/internal/middlewares"
	"github.com/goakili/akili/server/internal/models"
	"github.com/goakili/akili/server/internal/storage/pagination"
	"github.com/jkaninda/okapi"
)

func (h *Handlers) ListQuestions(c *okapi.Context) error {
	q := h.DB.Where("organization_id = ?", middlewares.OrgID(c))
	for _, f := range []string{"status", "task_id", "session_id"} {
		if v := c.Query(f); v != "" {
			q = q.Where(f+" = ?", v)
		}
	}
	p := pageParams(c)
	out, total, err := pagination.Find[models.Question](q, p, "created_at DESC, id DESC")
	if err != nil {
		return c.AbortInternalServerError("list failed", err)
	}
	return paged(c, out, total, p)
}

// AnswerRequest answers a question: one of the agent's options, or the person's own words.
type AnswerRequest struct {
	Body struct {
		Choice *int   `json:"choice" minimum:"0" description:"0-based index of the chosen option"`
		Text   string `json:"text" maxLength:"4000" description:"an answer in your own words, instead of an option"`
	} `json:"body"`
}

// AnswerQuestion records the answer and hands it to the waiting agent.
func (h *Handlers) AnswerQuestion(c *okapi.Context, req *AnswerRequest) error {
	q, err := h.Sessions.Answer(c.Request().Context(), middlewares.OrgID(c), c.Param("id"), middlewares.UserID(c), req.Body.Choice, req.Body.Text)
	if err != nil {
		return mapErr(c, err)
	}
	return ok(c, q)
}
