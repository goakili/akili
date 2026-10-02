// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package sessions

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/audit"
	"github.com/goakili/akili/server/internal/bus"
	"github.com/goakili/akili/server/internal/models"
)

// Event types for changes.
const EvChangeUpdated = "change.updated"

// evaluateChange checks a proposed plan: the change_run tool itself must be allowed, and every call
// in it must pass the policy. A plan always needs a human, whatever the autonomy level.
func (h *Hub) evaluateChange(pol proto.Policy, autonomy proto.Autonomy, workdir, base string, req proto.ToolRequest) (proto.Decision, *proto.ChangePlan) {
	d := proto.EvaluateAt(pol, autonomy, workdir, base, proto.Call{Tool: req.Tool, Input: req.Input})
	if d.Effect == proto.EffectDeny {
		return d, nil
	}
	plan, err := proto.ParseChangePlan(req.Input)
	if err != nil {
		return proto.Decision{Effect: proto.EffectDeny, Reason: "invalid change plan: " + err.Error(), Risk: d.Risk}, nil
	}
	maxRisk := proto.RiskLow
	for phase, calls := range map[string][]proto.ChangeCall{proto.PhaseStep: plan.Steps, proto.PhaseVerify: plan.Verify, proto.PhaseRollback: plan.Rollback} {
		for i, c := range calls {
			cd := proto.EvaluateAt(pol, autonomy, workdir, base, proto.Call{Tool: c.Tool, Input: c.Input})
			if cd.Effect == proto.EffectDeny {
				return proto.Decision{Effect: proto.EffectDeny, Risk: d.Risk, Reason: fmt.Sprintf("%s %d (%s): %s", phase, i+1, c.Tool, cd.Reason)}, nil
			}
			if cd.Risk > maxRisk {
				maxRisk = cd.Risk
			}
		}
	}
	return proto.Decision{Effect: proto.EffectApprove, Risk: maxRisk,
		Reason: fmt.Sprintf("change plan %q: %d steps, %d checks, %d rollback calls (highest risk %s)", plan.Title, len(plan.Steps), len(plan.Verify), len(plan.Rollback), maxRisk)}, &plan
}

// createChange records a proposed plan bound to its approval.
func (h *Hub) createChange(ctx context.Context, s *models.ChatSession, ap *models.Approval, plan *proto.ChangePlan, risk proto.Risk) error {
	ch := models.Change{Base: models.Base{ID: models.NewID("chg"), OrganizationID: s.OrganizationID}, SessionID: s.ID, TaskID: s.TaskID,
		AgentID: s.AgentID, ApprovalID: ap.ID, Title: plan.Title, Reason: plan.Reason, Risk: risk.String(), Status: proto.ChangePending}
	add := func(phase string, calls []proto.ChangeCall) {
		for _, c := range calls {
			spec, _ := proto.LookupTool(c.Tool)
			ch.Calls = append(ch.Calls, models.ChangeCall{Phase: phase, Tool: c.Tool, Input: c.Input, Description: c.Description, Expect: c.Expect, Reject: c.Reject,
				Hash: proto.CallHash(c.Tool, c.Input), Risk: spec.Risk.String(), Status: "pending"})
		}
	}
	add(proto.PhaseStep, plan.Steps)
	add(proto.PhaseVerify, plan.Verify)
	add(proto.PhaseRollback, plan.Rollback)
	if err := h.db.WithContext(ctx).Create(&ch).Error; err != nil {
		return err
	}
	ap.ChangeID = &ch.ID
	h.db.WithContext(ctx).Model(&models.Approval{}).Where("id = ?", ap.ID).Update("change_id", ch.ID)
	h.emitChange(ctx, &ch)
	return nil
}

// authorizeChangeCall allows a call only if it matches a pending call of the session's approved
// change exactly (same phase, tool and input). Each call can be used once.
func (h *Hub) authorizeChangeCall(ctx context.Context, s *models.ChatSession, req proto.ToolRequest) (bool, string) {
	var ch models.Change
	if err := h.db.WithContext(ctx).First(&ch, "id = ? AND session_id = ?", req.ChangeID, s.ID).Error; err != nil {
		return false, "unknown change"
	}
	if ch.Status != proto.ChangeApproved && ch.Status != proto.ChangeRunning {
		return false, "the change is " + ch.Status
	}
	hash := proto.CallHash(req.Tool, req.Input)
	for i := range ch.Calls {
		c := &ch.Calls[i]
		if c.Phase == req.Phase && c.Hash == hash && c.Status == "pending" {
			c.Status, c.RequestID = "running", req.RequestID
			// Conditional on the old calls value would be ideal; one agent stream per session makes
			// concurrent use of the same call impossible, so a plain save is enough.
			if err := h.db.WithContext(ctx).Model(&ch).Select("calls").Updates(&models.Change{Calls: ch.Calls}).Error; err != nil {
				return false, "could not record the change step"
			}
			return true, fmt.Sprintf("part of approved change %s (%s)", ch.ID, req.Phase)
		}
	}
	return false, "this call is not part of the approved change (or already ran)"
}

// recordChangeResult stores a change call's outcome.
func (h *Hub) recordChangeResult(ctx context.Context, s *models.ChatSession, r proto.ToolResult) {
	var ch models.Change
	if err := h.db.WithContext(ctx).First(&ch, "id = ? AND session_id = ?", r.ChangeID, s.ID).Error; err != nil {
		return
	}
	for i := range ch.Calls {
		c := &ch.Calls[i]
		if c.RequestID != r.RequestID || c.Status != "running" {
			continue
		}
		c.Status = "ok"
		// The control plane judges checks itself, on the full output, rather than trusting the agent.
		switch {
		case r.IsError:
			c.Status = "failed"
		case c.Phase == proto.PhaseVerify && c.Expect != "" && !strings.Contains(r.Output, c.Expect):
			c.Status = "failed"
		case c.Phase == proto.PhaseVerify && c.Reject != "" && strings.Contains(r.Output, c.Reject):
			c.Status = "failed"
		}
		c.Output = truncate(r.Output, 4000)
		c.DurationMs = r.DurationMs
		break
	}
	h.db.WithContext(ctx).Model(&ch).Select("calls").Updates(&models.Change{Calls: ch.Calls})
	h.emitChange(ctx, &ch)
}

// changeUpdate applies an agent's progress report.
func (h *Hub) changeUpdate(ctx context.Context, s *models.ChatSession, u proto.ChangeUpdate) {
	var ch models.Change
	if err := h.db.WithContext(ctx).First(&ch, "id = ? AND session_id = ?", u.ChangeID, s.ID).Error; err != nil {
		return
	}
	now := time.Now().UTC()
	updates := map[string]any{"status": u.Status, "detail": truncate(u.Detail, 500)}
	switch u.Status {
	case proto.ChangeRunning:
		if ch.Status != proto.ChangeApproved {
			return
		}
		updates["started_at"] = now
	case proto.ChangeSucceeded, proto.ChangeFailed, proto.ChangeRolledBack:
		if ch.Status != proto.ChangeRunning {
			return
		}
		updates["finished_at"] = now
		for i := range ch.Calls {
			if ch.Calls[i].Status == "pending" {
				ch.Calls[i].Status = "skipped"
			}
		}
		h.db.WithContext(ctx).Model(&ch).Select("calls").Updates(&models.Change{Calls: ch.Calls})
	default:
		return
	}
	h.db.WithContext(ctx).Model(&ch).Updates(updates)
	h.audit.Best(ctx, audit.Entry{OrganizationID: s.OrganizationID, ActorType: audit.ActorAgent, ActorID: s.AgentID,
		Action: "change." + u.Status, TargetType: "change", TargetID: ch.ID, Metadata: map[string]any{"title": ch.Title, "detail": u.Detail}})
	if u.Status == proto.ChangeFailed || u.Status == proto.ChangeRolledBack {
		h.notify.Send(fmt.Sprintf("Akili change %q %s: %s\n%s", ch.Title, u.Status, u.Detail, h.notify.Link("/changes/"+ch.ID)))
	}
	h.db.WithContext(ctx).First(&ch, "id = ?", ch.ID)
	h.emitChange(ctx, &ch)
}

// resolveChange follows its approval: approved, denied or expired.
func (h *Hub) resolveChange(ctx context.Context, approvalID, status string) *models.Change {
	var ch models.Change
	if err := h.db.WithContext(ctx).First(&ch, "approval_id = ?", approvalID).Error; err != nil {
		return nil
	}
	res := h.db.WithContext(ctx).Model(&models.Change{}).Where("id = ? AND status = ?", ch.ID, proto.ChangePending).Update("status", status)
	if res.RowsAffected == 0 {
		return nil
	}
	ch.Status = status
	h.emitChange(ctx, &ch)
	return &ch
}

func (h *Hub) emitChange(ctx context.Context, ch *models.Change) {
	b, _ := json.Marshal(ch)
	h.bus.Emit(ctx, ch.OrganizationID, bus.Event{Type: EvChangeUpdated, SessionID: ch.SessionID, AgentID: ch.AgentID, TaskID: deref(ch.TaskID), Data: b})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
