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

package models

// TaskKindReview runs with read-only tools only, whatever the reviewer's policy allows.
const TaskKindReview = "review"

const (
	ReviewTargetChange = "change"
	ReviewTargetTask   = "task"
)

const (
	ReviewPending = "pending"
	ReviewDone    = "done"
	ReviewFailed  = "failed"
)

// Review is a second agent's advice on a change plan or a task's work; it never decides anything.
type Review struct {
	Base
	TargetType      string   `gorm:"size:20;not null;index:idx_review_target" json:"target_type"`
	TargetID        string   `gorm:"size:40;not null;index:idx_review_target" json:"target_id"`
	AuthorAgentID   string   `gorm:"size:40" json:"author_agent_id"`
	ReviewerTaskID  *string  `gorm:"size:40;index" json:"reviewer_task_id"`
	ReviewerAgentID *string  `gorm:"size:40" json:"reviewer_agent_id"`
	Status          string   `gorm:"size:20;not null" json:"status"`
	Verdict         string   `gorm:"size:20" json:"verdict"`
	Summary         string   `gorm:"type:text" json:"summary"`
	Risks           []string `gorm:"type:jsonb;serializer:json" json:"risks"`
	Concerns        []string `gorm:"type:jsonb;serializer:json" json:"concerns"`
	RequestedBy     string   `gorm:"size:40" json:"requested_by"`
}
