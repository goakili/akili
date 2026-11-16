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
