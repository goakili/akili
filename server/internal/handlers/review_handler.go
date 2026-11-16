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
	"errors"

	"github.com/goakili/akili/server/internal/middlewares"
	"github.com/goakili/akili/server/internal/models"
	"github.com/goakili/akili/server/internal/reviews"
	"github.com/jkaninda/okapi"
)

type ReviewRequest struct {
	Body reviews.Input `json:"body"`
}

func (h *Handlers) RequestChangeReview(c *okapi.Context, req *ReviewRequest) error {
	return h.requestReview(c, models.ReviewTargetChange, req)
}

func (h *Handlers) RequestTaskReview(c *okapi.Context, req *ReviewRequest) error {
	return h.requestReview(c, models.ReviewTargetTask, req)
}

func (h *Handlers) requestReview(c *okapi.Context, targetType string, req *ReviewRequest) error {
	rv, err := h.Reviews.Request(c.Request().Context(), middlewares.OrgID(c), middlewares.UserID(c), targetType, c.Param("id"), req.Body)
	if errors.Is(err, reviews.ErrSameAgent) {
		return c.AbortBadRequest(err.Error())
	}
	if err != nil {
		return mapErr(c, err)
	}
	return created(c, rv)
}

func (h *Handlers) ListReviews(c *okapi.Context) error {
	rs, err := h.Reviews.List(c.Request().Context(), middlewares.OrgID(c), c.Query("target_type"), c.Query("target_id"))
	if err != nil {
		return mapErr(c, err)
	}
	return ok(c, rs)
}
