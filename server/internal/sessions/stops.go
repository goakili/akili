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
	"encoding/json"
	"fmt"
	"time"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/audit"
	"github.com/goakili/akili/server/internal/autonomy"
	"github.com/goakili/akili/server/internal/bus"
	"github.com/goakili/akili/server/internal/models"
)

const (
	stopContinue = "Continue with a different approach"
	stopEnd      = "Stop the task"
)

// review lets grants and change windows adjust the policy's decision; calls inside an approved change are already covered.
func (h *Hub) review(ctx context.Context, s *models.ChatSession, agent *models.Agent, req proto.ToolRequest, plan *proto.ChangePlan, d proto.Decision) autonomy.Outcome {
	if h.autonomy == nil || req.ChangeID != "" {
		return autonomy.Outcome{Decision: d}
	}
	return h.autonomy.Review(ctx, s.OrganizationID, agent, req.Tool, req.Input, plan, d)
}

// stopReason looks only at calls since the last stop question, so an operator's "continue" starts a fresh count.
func (h *Hub) stopReason(ctx context.Context, s *models.ChatSession, req proto.ToolRequest) string {
	set := models.AutonomySettings{}.WithDefaults()
	if h.autonomy != nil {
		set = h.autonomy.Settings(ctx, s.OrganizationID)
	}
	var since time.Time
	var last models.Question
	if h.db.WithContext(ctx).Where("session_id = ? AND kind = ?", s.ID, models.QuestionStop).Order("created_at DESC").First(&last).Error == nil {
		since = last.CreatedAt
	}
	var evs []models.SessionEvent
	h.db.WithContext(ctx).Where("session_id = ? AND type IN ? AND created_at > ?", s.ID, []string{EvToolRequest, EvToolResult}, since).
		Order("id DESC").Limit(1000).Find(&evs)

	type callInfo struct{ tool, hash string }
	calls := map[string]callInfo{}
	requests := 0
	for _, ev := range evs {
		if ev.Type != EvToolRequest {
			continue
		}
		var r struct {
			RequestID string          `json:"request_id"`
			Tool      string          `json:"tool"`
			Input     json.RawMessage `json:"input"`
		}
		if json.Unmarshal(ev.Payload, &r) != nil || r.Tool == proto.ToolAskUser {
			continue
		}
		calls[r.RequestID] = callInfo{r.Tool, proto.CallHash(r.Tool, r.Input)}
		requests++
	}
	if requests >= set.StopMaxToolCalls {
		return fmt.Sprintf("it made %d tool calls (the limit is %d)", requests, set.StopMaxToolCalls)
	}
	hash := proto.CallHash(req.Tool, req.Input)
	same, streak, streakOpen := 0, 0, true
	for _, ev := range evs {
		if ev.Type != EvToolResult {
			continue
		}
		var r proto.ToolResult
		if json.Unmarshal(ev.Payload, &r) != nil || r.Tool == proto.ToolAskUser {
			continue
		}
		if r.IsError && calls[r.RequestID].hash == hash {
			same++
		}
		if streakOpen && r.IsError {
			streak++
		} else {
			streakOpen = false
		}
	}
	switch {
	case same >= set.StopRepeatFailures:
		return fmt.Sprintf("the same %s call already failed %d times", req.Tool, same)
	case streak >= set.StopErrorStreak:
		return fmt.Sprintf("its last %d tool calls all failed", streak)
	}
	return ""
}

// pauseForOperator holds the call behind a question; whatever the answer, the held call never runs.
func (h *Hub) pauseForOperator(ctx context.Context, s *models.ChatSession, req proto.ToolRequest, reason string) proto.ToolDecision {
	q := models.Question{Base: models.Base{ID: models.NewID("qst"), OrganizationID: s.OrganizationID}, SessionID: s.ID, TaskID: s.TaskID,
		AgentID: s.AgentID, RequestID: req.RequestID, Kind: models.QuestionStop, Status: models.QuestionPending, ExpiresAt: time.Now().UTC().Add(QuestionTTL),
		Question: fmt.Sprintf("Akili paused this task before another %s call because %s. How should the agent go on?", req.Tool, reason),
		Options:  []proto.AskUserOption{{Label: stopContinue, Description: "The paused call is skipped; the agent must try something else."}, {Label: stopEnd, Description: "The agent stops and reports what blocked it."}}}
	if err := h.db.WithContext(ctx).Create(&q).Error; err != nil {
		return proto.ToolDecision{RequestID: req.RequestID, Effect: proto.EffectDeny, Reason: "paused by Akili: " + reason + ". Stop and start your final message with \"BLOCKED:\""}
	}
	h.audit.Best(ctx, audit.Entry{OrganizationID: s.OrganizationID, ActorType: audit.ActorSystem, Action: "task.paused", TargetType: "session", TargetID: s.ID,
		Metadata: map[string]any{"task_id": deref(s.TaskID), "tool": req.Tool, "reason": reason, "question_id": q.ID}})
	h.pauseTask(ctx, s.TaskID)
	h.bus.EmitData(ctx, s.OrganizationID, bus.Event{Type: EvQuestionCreated, SessionID: s.ID, AgentID: s.AgentID, TaskID: deref(s.TaskID)}, q)
	h.addEvent(ctx, s, EvQuestionCreated, q)
	link := "/sessions/" + s.ID
	if s.TaskID != nil {
		link = "/tasks/" + *s.TaskID
	}
	h.notify.Send(fmt.Sprintf("Akili: paused a task: %s\n%s", reason, h.notify.Link(link)))
	return proto.ToolDecision{RequestID: req.RequestID, Effect: proto.EffectApprove, Reason: "paused by Akili: " + reason + "; waiting for an operator"}
}

// stopDecision is what the agent hears once a stop question is answered or expires: always a denial of the held call.
func stopDecision(q *models.Question, expired bool) proto.ToolDecision {
	var why string
	switch {
	case expired:
		why = "Akili paused this call because the task looked stuck, and nobody answered. Stop and start your final message with \"BLOCKED:\" and what failed"
	case q.Choice != nil && q.Answer == stopEnd:
		why = "Akili paused this call and the operator stopped the task. Make no more tool calls; start your final message with \"BLOCKED:\" and what failed"
	case q.Choice != nil:
		why = "Akili paused this call because the task looked stuck; the operator asked you to continue with a different approach"
	default:
		why = "Akili paused this call because the task looked stuck. The operator replied:\n" + q.Answer
	}
	return proto.ToolDecision{RequestID: q.RequestID, Effect: proto.EffectDeny, Reason: why}
}

// grantedChange records a change plan a runbook grant allowed, as an approval decided by the grant rather than a person.
func (h *Hub) grantedChange(ctx context.Context, s *models.ChatSession, agent *models.Agent, req proto.ToolRequest, inputHash string, plan *proto.ChangePlan, d proto.Decision) (string, error) {
	now := time.Now().UTC()
	ap := models.Approval{Base: models.Base{ID: models.NewID("apr"), OrganizationID: s.OrganizationID}, SessionID: s.ID, TaskID: s.TaskID,
		AgentID: agent.ID, RequestID: req.RequestID, Tool: req.Tool, Input: req.Input, InputHash: inputHash, Risk: d.Risk.String(),
		Reason: d.Reason, Status: models.ApprovalPending, ExpiresAt: now.Add(ApprovalTTL), Note: d.Reason}
	if err := h.db.WithContext(ctx).Create(&ap).Error; err != nil {
		return "", err
	}
	if err := h.createChange(ctx, s, &ap, plan, d.Risk); err != nil {
		return "", err
	}
	h.db.WithContext(ctx).Model(&models.Approval{}).Where("id = ?", ap.ID).Updates(map[string]any{"status": models.ApprovalApproved, "decided_at": now})
	ch := h.resolveChange(ctx, ap.ID, proto.ChangeApproved)
	if ch == nil {
		return "", fmt.Errorf("change for approval %s not found", ap.ID)
	}
	return ch.ID, nil
}
