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
