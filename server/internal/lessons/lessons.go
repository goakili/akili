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

// Package lessons is the agents' memory across sessions; nothing an agent proposes reaches a prompt until a human approves it.
package lessons

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/audit"
	"github.com/goakili/akili/server/internal/models"
	"github.com/goakili/akili/server/internal/storage/pagination"
	"gorm.io/gorm"
)

// MaxInPrompt bounds how many approved lessons enter a system prompt (most specific, then newest, first).
const MaxInPrompt = 20

// maxPromptChars caps the lessons' share of a system prompt; postmortems can be long.
const maxPromptChars = 6000

// maxPerSession limits proposals so a looping or manipulated model cannot flood the review queue.
const maxPerSession = 5

type Service struct {
	db    *gorm.DB
	audit *audit.Logger
}

func New(db *gorm.DB, a *audit.Logger) *Service { return &Service{db: db, audit: a} }

var (
	ErrNotFound = errors.New("lesson not found")
	ErrInvalid  = errors.New("a lesson must be 10-400 characters (a postmortem up to 2000)")
)

// RunRemote executes lesson_propose for a session (a remote tool runner).
func (s *Service) RunRemote(ctx context.Context, sess *models.ChatSession, _ string, input json.RawMessage) proto.RemoteResult {
	var in proto.LessonInput
	if err := json.Unmarshal(input, &in); err != nil {
		return proto.RemoteResult{Output: "error: invalid input", IsError: true}
	}
	text := clean(in.Lesson)
	if in.Kind == "lesson" {
		in.Kind = ""
	}
	var n int64
	s.db.WithContext(ctx).Model(&models.Lesson{}).Where("session_id = ?", sess.ID).Count(&n)
	if n >= maxPerSession {
		return proto.RemoteResult{Output: "error: this session already proposed the maximum number of lessons", IsError: true}
	}
	var dup int64
	s.db.WithContext(ctx).Model(&models.Lesson{}).Where("organization_id = ? AND (agent_id = ? OR agent_id IS NULL) AND lower(text) = ? AND status <> ?",
		sess.OrganizationID, sess.AgentID, strings.ToLower(text), models.LessonRejected).Count(&dup)
	if dup > 0 {
		return proto.RemoteResult{Output: "This lesson is already recorded."}
	}
	agent := sess.AgentID
	l := &models.Lesson{Base: models.Base{ID: models.NewID("lsn"), OrganizationID: sess.OrganizationID}, AgentID: &agent, Text: text,
		Status: models.LessonProposed, SessionID: &sess.ID, TaskID: sess.TaskID, ProposedBy: sess.AgentID,
		Kind: in.Kind, App: clean(in.App), Alert: clean(in.Alert), ProjectID: sess.ProjectID}
	if err := s.db.WithContext(ctx).Create(l).Error; err != nil {
		return proto.RemoteResult{Output: "error: could not store the lesson", IsError: true}
	}
	s.audit.Best(ctx, audit.Entry{OrganizationID: sess.OrganizationID, ActorType: audit.ActorAgent, ActorID: sess.AgentID,
		Action: "lesson.propose", TargetType: "lesson", TargetID: l.ID, Metadata: map[string]any{"session_id": sess.ID}})
	return proto.RemoteResult{Output: "Proposed for operator review (" + l.ID + "). It will be used in future sessions once approved."}
}

// clean collapses whitespace: a lesson is one line in the prompt.
func clean(s string) string { return strings.Join(strings.Fields(s), " ") }

func valid(text, kind string) bool {
	limit := proto.MaxLessonLen
	if kind == proto.LessonKindPostmortem {
		limit = proto.MaxPostmortemLen
	}
	n := len([]rune(text))
	return n >= 10 && n <= limit
}

// ForPrompt recalls the approved lessons that fit a session: unscoped ones, and those whose app, alert, host label or project match.
func (s *Service) ForPrompt(ctx context.Context, org string, q models.LessonQuery) []models.PromptLesson {
	var rows []models.Lesson
	db := s.db.WithContext(ctx).Where("organization_id = ? AND status = ? AND (agent_id = ? OR agent_id IS NULL)", org, models.LessonApproved, q.AgentID)
	if q.ProjectID != "" {
		db = db.Where("project_id IS NULL OR project_id = ?", q.ProjectID)
	} else {
		db = db.Where("project_id IS NULL")
	}
	db.Order("decided_at DESC").Limit(500).Find(&rows)
	text := strings.ToLower(q.Text)
	type scored struct {
		l     models.Lesson
		score int
	}
	var fit []scored
	for _, l := range rows {
		// A scoped lesson needs one of its app, alert or host label to match; more matches rank it higher.
		scoped, score := false, 0
		for _, scope := range []struct {
			value   string
			matches func(string) bool
		}{
			{l.App, func(v string) bool { return strings.Contains(text, strings.ToLower(v)) }},
			{l.Alert, func(v string) bool { return strings.Contains(text, strings.ToLower(v)) }},
			{l.Host, func(v string) bool { return slices.Contains(q.Labels, v) }},
		} {
			if scope.value != "" {
				scoped = true
				if scope.matches(scope.value) {
					score++
				}
			}
		}
		if scoped && score == 0 {
			continue
		}
		if l.ProjectID != nil {
			score++
		}
		fit = append(fit, scored{l, score})
	}
	sort.SliceStable(fit, func(i, j int) bool { return fit[i].score > fit[j].score })
	var out []models.PromptLesson
	used := 0
	for _, f := range fit {
		line := promptLine(&f.l)
		if len(out) >= MaxInPrompt || used+len(line) > maxPromptChars {
			break
		}
		used += len(line)
		out = append(out, models.PromptLesson{ID: f.l.ID, Text: line})
	}
	return out
}

func promptLine(l *models.Lesson) string {
	if l.Kind != proto.LessonKindPostmortem {
		return l.Text
	}
	var scope []string
	for _, v := range []string{l.App, l.Alert} {
		if v != "" {
			scope = append(scope, v)
		}
	}
	if len(scope) == 0 {
		return "Postmortem: " + l.Text
	}
	return fmt.Sprintf("Postmortem (%s): %s", strings.Join(scope, ", "), l.Text)
}

type Filter struct {
	Status  string
	AgentID string
}

// List returns one page of lessons, newest first, and the number of matching lessons.
func (s *Service) List(ctx context.Context, org string, f Filter, p pagination.Page) ([]models.Lesson, int64, error) {
	q := s.db.WithContext(ctx).Where("organization_id = ?", org)
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.AgentID != "" {
		q = q.Where("agent_id = ?", f.AgentID)
	}
	return pagination.Find[models.Lesson](q, p, "created_at DESC, id DESC")
}

// Scope limits where an operator-written lesson is recalled; the zero value recalls it everywhere.
type Scope struct {
	Kind      string
	App       string
	Alert     string
	Host      string
	ProjectID *string
}

// Create adds an operator-written lesson, approved at once. agentID nil applies it to every agent.
func (s *Service) Create(ctx context.Context, org, userID string, agentID *string, text string, scope Scope) (*models.Lesson, error) {
	text = clean(text)
	if scope.Kind != "" && scope.Kind != proto.LessonKindPostmortem {
		return nil, errors.New("kind must be empty or postmortem")
	}
	if !valid(text, scope.Kind) {
		return nil, ErrInvalid
	}
	now := time.Now().UTC()
	l := &models.Lesson{Base: models.Base{ID: models.NewID("lsn"), OrganizationID: org}, AgentID: agentID, Text: text,
		Status: models.LessonApproved, ProposedBy: userID, DecidedBy: &userID, DecidedAt: &now,
		Kind: scope.Kind, App: clean(scope.App), Alert: clean(scope.Alert), Host: clean(scope.Host), ProjectID: scope.ProjectID}
	if err := s.db.WithContext(ctx).Create(l).Error; err != nil {
		return nil, err
	}
	return l, s.audit.Record(ctx, audit.Entry{OrganizationID: org, ActorType: audit.ActorUser, ActorID: userID, Action: "lesson.create",
		TargetType: "lesson", TargetID: l.ID, Metadata: map[string]any{"agent_id": agentID, "text": text}})
}

// Decide approves (optionally with edited text) or rejects a lesson; the audit records the exact text that will reach prompts.
func (s *Service) Decide(ctx context.Context, org, userID, id string, approve bool, text, note string) (*models.Lesson, error) {
	var l models.Lesson
	if err := s.db.WithContext(ctx).First(&l, "id = ? AND organization_id = ?", id, org).Error; err != nil {
		return nil, ErrNotFound
	}
	status := models.LessonRejected
	if approve {
		status = models.LessonApproved
		if text = clean(text); text == "" {
			text = l.Text
		}
		if !valid(text, l.Kind) {
			return nil, ErrInvalid
		}
		l.Text = text
	}
	now := time.Now().UTC()
	l.Status, l.DecidedBy, l.DecidedAt, l.Note = status, &userID, &now, note
	if err := s.audit.Record(ctx, audit.Entry{OrganizationID: org, ActorType: audit.ActorUser, ActorID: userID, Action: "lesson." + status,
		TargetType: "lesson", TargetID: l.ID, Metadata: map[string]any{"text": l.Text, "agent_id": l.AgentID, "note": note}}); err != nil {
		return nil, err
	}
	return &l, s.db.WithContext(ctx).Model(&l).Select("text", "status", "decided_by", "decided_at", "note").Updates(&l).Error
}

// Delete removes a lesson; it leaves future prompts.
func (s *Service) Delete(ctx context.Context, org, userID, id string) error {
	res := s.db.WithContext(ctx).Where("id = ? AND organization_id = ?", id, org).Delete(&models.Lesson{})
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	s.audit.Best(ctx, audit.Entry{OrganizationID: org, ActorType: audit.ActorUser, ActorID: userID, Action: "lesson.delete", TargetType: "lesson", TargetID: id})
	return nil
}
