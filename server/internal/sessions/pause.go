// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

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

// resumeTask restarts the clock once nothing in the session waits on a person any more, and returns
// the task's new deadline for the agent (nil when it has none or still waits).
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
