// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/audit"
	"github.com/goakili/akili/server/internal/bus"
	"github.com/goakili/akili/server/internal/models"
	"github.com/jkaninda/logger"
)

// QuestionTTL is how long an ask_user question waits for an answer. The task's own timeout still
// applies, so a question on a task with a shorter timeout ends with the task.
const QuestionTTL = 24 * time.Hour

// MaxAnswerLen bounds an answer written in the person's own words.
const MaxAnswerLen = 4000

// Session events for questions.
const (
	EvQuestionCreated  = "question.created"
	EvQuestionResolved = "question.resolved"
)

// Question errors.
var (
	ErrQuestionClosed = errors.New("question is no longer waiting for an answer")
	ErrBadAnswer      = errors.New("pick one option or write an answer, not both")
)

// askUser records an allowed ask_user call. The agent gets its decision only when someone answers or
// the question expires, so nothing is returned on success; a failure is a deny the agent sees now.
func (h *Hub) askUser(ctx context.Context, s *models.ChatSession, req proto.ToolRequest) *proto.ToolDecision {
	deny := func(reason string) *proto.ToolDecision {
		return &proto.ToolDecision{RequestID: req.RequestID, Effect: proto.EffectDeny, Reason: reason}
	}
	var in proto.AskUserInput
	if err := json.Unmarshal(req.Input, &in); err != nil {
		return deny("invalid question")
	}
	// Model text shown to people and kept in the database: strip anything that looks like a credential.
	opts := make([]proto.AskUserOption, len(in.Options))
	for i, o := range in.Options {
		opts[i] = proto.AskUserOption{Label: proto.Redact(strings.TrimSpace(o.Label)), Description: proto.Redact(strings.TrimSpace(o.Description)),
			Recommended: o.Recommended}
	}
	q := models.Question{Base: models.Base{ID: models.NewID("qst"), OrganizationID: s.OrganizationID}, SessionID: s.ID, TaskID: s.TaskID,
		AgentID: s.AgentID, RequestID: req.RequestID, Question: proto.Redact(strings.TrimSpace(in.Question)), Options: opts,
		Status: models.QuestionPending, ExpiresAt: time.Now().UTC().Add(QuestionTTL)}
	if err := h.db.WithContext(ctx).Create(&q).Error; err != nil {
		return deny("could not record the question")
	}
	h.pauseTask(ctx, s.TaskID)
	h.bus.EmitData(ctx, s.OrganizationID, bus.Event{Type: EvQuestionCreated, SessionID: s.ID, AgentID: s.AgentID, TaskID: deref(s.TaskID)}, q)
	h.addEvent(ctx, s, EvQuestionCreated, q)
	link := "/sessions/" + s.ID
	if s.TaskID != nil {
		link = "/tasks/" + *s.TaskID
	}
	var agent models.Agent
	h.db.WithContext(ctx).Select("name").First(&agent, "id = ?", s.AgentID)
	h.notify.Send(fmt.Sprintf("Akili: agent %q asks you to decide: %s\n%s", agent.Name, truncate(q.Question, 300), h.notify.Link(link)))
	return nil
}

// Answer records a person's answer to a pending question and hands it to the waiting agent. Exactly
// one of choice (a 0-based option) and text (their own words) is given.
func (h *Hub) Answer(ctx context.Context, org, questionID, userID string, choice *int, text string) (*models.Question, error) {
	text = strings.TrimSpace(text)
	if (choice == nil) == (text == "") || len([]rune(text)) > MaxAnswerLen {
		return nil, ErrBadAnswer
	}
	var q models.Question
	if err := h.db.WithContext(ctx).First(&q, "id = ? AND organization_id = ?", questionID, org).Error; err != nil {
		return nil, ErrNotFound
	}
	if choice != nil && (*choice < 0 || *choice >= len(q.Options)) {
		return nil, ErrBadAnswer
	}
	now := time.Now().UTC()
	if q.Status == models.QuestionPending && now.After(q.ExpiresAt) {
		h.expireQuestion(ctx, &q, "the question expired")
		return nil, ErrQuestionClosed
	}
	answer := text
	if choice != nil {
		answer = q.Options[*choice].Label
	}
	// Conditional update: two people answering at once cannot both win.
	res := h.db.WithContext(ctx).Model(&models.Question{}).Where("id = ? AND status = ?", q.ID, models.QuestionPending).
		Updates(map[string]any{"status": models.QuestionAnswered, "choice": choice, "answer": answer, "answered_by": userID, "answered_at": now})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrQuestionClosed
	}
	q.Status, q.Choice, q.Answer, q.AnsweredBy, q.AnsweredAt = models.QuestionAnswered, choice, answer, &userID, &now
	reopen := func() {
		h.db.WithContext(ctx).Model(&models.Question{}).Where("id = ?", q.ID).
			Updates(map[string]any{"status": models.QuestionPending, "choice": nil, "answer": "", "answered_by": nil, "answered_at": nil})
		h.pauseTask(ctx, q.TaskID)
	}
	if err := h.audit.Record(ctx, audit.Entry{OrganizationID: org, ActorType: audit.ActorUser, ActorID: userID,
		Action: "question.answered", TargetType: "question", TargetID: q.ID, Metadata: map[string]any{
			"session_id": q.SessionID, "task_id": deref(q.TaskID), "choice": choice, "own_words": choice == nil}}); err != nil {
		reopen()
		return nil, fmt.Errorf("audit unavailable: %w", err)
	}
	d := proto.ToolDecision{RequestID: q.RequestID, Effect: proto.EffectAllow, Reason: "answered",
		Result: &proto.RemoteResult{Output: answerText(&q)}, Deadline: h.resumeTask(ctx, q.SessionID, q.TaskID)}
	if err := h.bus.SendCommandData(ctx, q.AgentID, bus.Command{Type: bus.CmdToolDecision, SessionID: q.SessionID}, d); err != nil {
		// The agent cannot receive it now; leave the question open rather than lose the answer.
		reopen()
		return nil, err
	}
	h.resolved(ctx, &q)
	return &q, nil
}

// answerText is the tool result the model reads.
func answerText(q *models.Question) string {
	if q.Choice != nil {
		return fmt.Sprintf("The user chose option %d: %q.", *q.Choice+1, q.Answer)
	}
	return "The user did not pick one of your options and answered in their own words:\n" + q.Answer
}

// ExpireQuestions ends questions past their deadline; the agent is told nobody answered.
func (h *Hub) ExpireQuestions(ctx context.Context) {
	var qs []models.Question
	h.db.WithContext(ctx).Where("status = ? AND expires_at < ?", models.QuestionPending, time.Now().UTC()).Limit(100).Find(&qs)
	for i := range qs {
		h.expireQuestion(ctx, &qs[i], "no answer before the question expired")
	}
}

func (h *Hub) expireQuestion(ctx context.Context, q *models.Question, why string) {
	res := h.db.WithContext(ctx).Model(&models.Question{}).Where("id = ? AND status = ?", q.ID, models.QuestionPending).
		Update("status", models.QuestionExpired)
	if res.RowsAffected == 0 {
		return
	}
	q.Status = models.QuestionExpired
	h.audit.Best(ctx, audit.Entry{OrganizationID: q.OrganizationID, ActorType: audit.ActorSystem, Action: "question.expired",
		TargetType: "question", TargetID: q.ID, Metadata: map[string]any{"reason": why}})
	h.resolved(ctx, q)
	out := "No answer: " + why + ". Stop and start your final message with \"BLOCKED:\" followed by the question."
	if r := q.Recommended(); r != nil {
		out = fmt.Sprintf("No answer: %s. If your recommended option %q is safe and easy to undo, continue with it and say so in your summary; "+
			"otherwise stop and start your final message with \"BLOCKED:\" followed by the question.", why, r.Label)
	}
	if err := h.bus.SendCommandData(ctx, q.AgentID, bus.Command{Type: bus.CmdToolDecision, SessionID: q.SessionID},
		proto.ToolDecision{RequestID: q.RequestID, Effect: proto.EffectAllow, Reason: "expired", Result: &proto.RemoteResult{Output: out},
			Deadline: h.resumeTask(ctx, q.SessionID, q.TaskID)}); err != nil {
		logger.Debug("question expired; agent not reachable", "question", q.ID, "error", err)
	}
}

// expireSessionQuestions closes the questions of a stream that ended: nobody waits for their answer.
func (h *Hub) expireSessionQuestions(ctx context.Context, sessionID, reason string) {
	var qs []models.Question
	h.db.WithContext(ctx).Where("session_id = ? AND status = ?", sessionID, models.QuestionPending).Find(&qs)
	for i := range qs {
		h.expireQuestion(ctx, &qs[i], "session stream ended: "+reason)
	}
}

func (h *Hub) resolved(ctx context.Context, q *models.Question) {
	h.bus.EmitData(ctx, q.OrganizationID, bus.Event{Type: EvQuestionResolved, SessionID: q.SessionID, AgentID: q.AgentID, TaskID: deref(q.TaskID)}, q)
	var s models.ChatSession
	if h.db.WithContext(ctx).First(&s, "id = ?", q.SessionID).Error == nil {
		h.addEvent(ctx, &s, EvQuestionResolved, q)
	}
}
