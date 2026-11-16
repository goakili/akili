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
	"strings"

	"github.com/goakili/akili/server/internal/models"
)

// recallLessons picks the lessons for a new session and records which, so a reader can see what the agent was told.
func (h *Hub) recallLessons(ctx context.Context, s *models.ChatSession, agent *models.Agent) []string {
	if h.lessons == nil {
		return nil
	}
	q := models.LessonQuery{AgentID: agent.ID, Labels: agent.Labels, ProjectID: deref(s.ProjectID)}
	if s.TaskID != nil {
		var t models.Task
		if h.db.WithContext(ctx).Select("title", "goal").First(&t, "id = ?", *s.TaskID).Error == nil {
			q.Text = t.Title + "\n" + t.Goal
		}
	}
	recalled := h.lessons(ctx, s.OrganizationID, q)
	if len(recalled) == 0 {
		return nil
	}
	texts, ids := make([]string, len(recalled)), make([]string, len(recalled))
	for i, l := range recalled {
		texts[i], ids[i] = l.Text, l.ID
	}
	h.addEvent(ctx, s, EvLessonsRecalled, map[string]any{"lesson_ids": ids, "summary": strings.Join(texts, "\n")})
	return texts
}
