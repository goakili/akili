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
