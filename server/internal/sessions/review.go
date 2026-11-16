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

package sessions

import (
	"context"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/models"
)

// isReview reads the task kind from the database each time, so a reviewer cannot be talked out of read-only mode.
func (h *Hub) isReview(ctx context.Context, s *models.ChatSession) bool {
	if s.TaskID == nil {
		return false
	}
	var t models.Task
	return h.db.WithContext(ctx).Select("kind").First(&t, "id = ?", *s.TaskID).Error == nil && t.Kind == models.TaskKindReview
}

func reviewDenial(tool string) string {
	if tool == proto.ToolReviewSubmit {
		return "review_submit is only for review tasks"
	}
	return "reviewers are read-only: " + tool + " is not available in a review"
}
