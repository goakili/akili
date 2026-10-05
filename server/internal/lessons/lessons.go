// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package lessons is the agents' memory across sessions ("soul"): agents propose short lessons with
// lesson_propose, operators approve them, and approved lessons are added to new sessions' system
// prompts. Nothing an agent proposes changes behaviour until a human accepts it.
package lessons

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/audit"
	"github.com/goakili/akili/server/internal/models"
	"github.com/goakili/akili/server/internal/storage/pagination"
	"gorm.io/gorm"
)

// MaxInPrompt bounds how many approved lessons enter a system prompt (newest first).
const MaxInPrompt = 20

// maxPerSession limits proposals so a looping or manipulated model cannot flood the review queue.
const maxPerSession = 5

// Service stores and reviews lessons.
type Service struct {
	db    *gorm.DB
	audit *audit.Logger
}

// New returns the service.
func New(db *gorm.DB, a *audit.Logger) *Service { return &Service{db: db, audit: a} }

// Errors.
var (
	ErrNotFound = errors.New("lesson not found")
	ErrInvalid  = errors.New("a lesson must be 10-400 characters")
)

// RunRemote executes lesson_propose for a session (a remote tool runner).
func (s *Service) RunRemote(ctx context.Context, sess *models.ChatSession, _ string, input json.RawMessage) proto.RemoteResult {
	var in proto.LessonInput
	if err := json.Unmarshal(input, &in); err != nil {
		return proto.RemoteResult{Output: "error: invalid input", IsError: true}
	}
	text := clean(in.Lesson)
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
		Status: models.LessonProposed, SessionID: &sess.ID, TaskID: sess.TaskID, ProposedBy: sess.AgentID}
	if err := s.db.WithContext(ctx).Create(l).Error; err != nil {
		return proto.RemoteResult{Output: "error: could not store the lesson", IsError: true}
	}
	s.audit.Best(ctx, audit.Entry{OrganizationID: sess.OrganizationID, ActorType: audit.ActorAgent, ActorID: sess.AgentID,
		Action: "lesson.propose", TargetType: "lesson", TargetID: l.ID, Metadata: map[string]any{"session_id": sess.ID}})
	return proto.RemoteResult{Output: "Proposed for operator review (" + l.ID + "). It will be used in future sessions once approved."}
}

// clean collapses whitespace: a lesson is one line in the prompt.
func clean(s string) string { return strings.Join(strings.Fields(s), " ") }

func valid(text string) bool {
	n := len([]rune(text))
	return n >= 10 && n <= proto.MaxLessonLen
}

// ForPrompt returns the approved lessons for an agent: its own and the organization-wide ones.
func (s *Service) ForPrompt(ctx context.Context, org, agentID string) []string {
	var rows []models.Lesson
	s.db.WithContext(ctx).Where("organization_id = ? AND status = ? AND (agent_id = ? OR agent_id IS NULL)", org, models.LessonApproved, agentID).
		Order("decided_at DESC").Limit(MaxInPrompt).Find(&rows)
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Text
	}
	return out
}

// Filter narrows List.
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

// Create adds an operator-written lesson, approved at once. agentID nil applies it to every agent.
func (s *Service) Create(ctx context.Context, org, userID string, agentID *string, text string) (*models.Lesson, error) {
	text = clean(text)
	if !valid(text) {
		return nil, ErrInvalid
	}
	now := time.Now().UTC()
	l := &models.Lesson{Base: models.Base{ID: models.NewID("lsn"), OrganizationID: org}, AgentID: agentID, Text: text,
		Status: models.LessonApproved, ProposedBy: userID, DecidedBy: &userID, DecidedAt: &now}
	if err := s.db.WithContext(ctx).Create(l).Error; err != nil {
		return nil, err
	}
	return l, s.audit.Record(ctx, audit.Entry{OrganizationID: org, ActorType: audit.ActorUser, ActorID: userID, Action: "lesson.create",
		TargetType: "lesson", TargetID: l.ID, Metadata: map[string]any{"agent_id": agentID, "text": text}})
}

// Decide approves (optionally with edited text) or rejects a lesson. The audit entry records the
// exact text that will reach prompts.
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
		if !valid(text) {
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
