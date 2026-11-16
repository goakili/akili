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
