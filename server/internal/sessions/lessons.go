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
