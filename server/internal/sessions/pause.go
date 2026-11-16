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
	"time"

	"github.com/goakili/akili/server/internal/models"
)

// pauseTask stops a task's timeout clock when it starts waiting on a person.
func (h *Hub) pauseTask(ctx context.Context, taskID *string) {
	if taskID == nil {
		return
	}
	h.db.WithContext(ctx).Model(&models.Task{}).Where("id = ? AND paused_at IS NULL", *taskID).Update("paused_at", time.Now().UTC())
}

// resumeTask returns the task's new deadline, or nil when it has none or still waits on a person.
func (h *Hub) resumeTask(ctx context.Context, sessionID string, taskID *string) *time.Time {
	if taskID == nil {
		return nil
	}
	var approvals, questions int64
	h.db.WithContext(ctx).Model(&models.Approval{}).Where("session_id = ? AND status = ?", sessionID, models.ApprovalPending).Count(&approvals)
	h.db.WithContext(ctx).Model(&models.Question{}).Where("session_id = ? AND status = ?", sessionID, models.QuestionPending).Count(&questions)
	if approvals+questions > 0 {
		return nil
	}
	var t models.Task
	if h.db.WithContext(ctx).First(&t, "id = ?", *taskID).Error != nil {
		return nil
	}
	if t.PausedAt != nil {
		waited := int(time.Since(*t.PausedAt).Round(time.Second) / time.Second)
		// Conditional on the pause we read, so two resolutions at once count the wait only once.
		res := h.db.WithContext(ctx).Model(&models.Task{}).Where("id = ? AND paused_at = ?", t.ID, *t.PausedAt).
			Updates(map[string]any{"paused_at": nil, "paused_sec": t.PausedSec + waited})
		if res.RowsAffected == 0 {
			h.db.WithContext(ctx).First(&t, "id = ?", t.ID)
		} else {
			t.PausedAt, t.PausedSec = nil, t.PausedSec+waited
		}
	}
	if at, ok := t.Deadline(); ok {
		return &at
	}
	return nil
}
