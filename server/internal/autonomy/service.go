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

package autonomy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/audit"
	"github.com/goakili/akili/server/internal/models"
	"gorm.io/gorm"
)

type auditor interface {
	Record(ctx context.Context, e audit.Entry) error
	Best(ctx context.Context, e audit.Entry)
}

type Service struct {
	db    *gorm.DB
	audit auditor
	now   func() time.Time
}

func New(db *gorm.DB, a *audit.Logger) *Service {
	return &Service{db: db, audit: a, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) Settings(ctx context.Context, org string) models.AutonomySettings {
	var o models.Organization
	s.db.WithContext(ctx).Select("autonomy").First(&o, "id = ?", org)
	return o.Autonomy.WithDefaults()
}

func (s *Service) SetSettings(ctx context.Context, org, userID string, in models.AutonomySettings) (models.AutonomySettings, error) {
	for _, v := range []int{in.GrantMaxHours, in.StopRepeatFailures, in.StopErrorStreak, in.StopMaxToolCalls, in.SuggestMinApprovals, in.SuggestMinApprovers, in.SuggestMinDays} {
		if v < 0 {
			return in, errors.New("settings cannot be negative")
		}
	}
	if in.GrantMaxHours > models.GrantHardMaxHours {
		return in, fmt.Errorf("grants can last at most %d hours", models.GrantHardMaxHours)
	}
	if err := s.audit.Record(ctx, audit.Entry{OrganizationID: org, ActorType: audit.ActorUser, ActorID: userID, Action: "settings.autonomy",
		TargetType: "organization", TargetID: org, Metadata: map[string]any{"settings": in}}); err != nil {
		return in, fmt.Errorf("audit unavailable: %w", err)
	}
	if err := s.db.WithContext(ctx).Model(&models.Organization{}).Where("id = ?", org).Update("autonomy", in).Error; err != nil {
		return in, err
	}
	return in.WithDefaults(), nil
}

// Outcome is a reviewed decision and what changed it, for the audit record.
type Outcome struct {
	Decision proto.Decision
	GrantID  string
	WindowID string
}

// Review runs after the policy check; it can only turn approve into allow (a grant) or allow into approve (a freeze).
func (s *Service) Review(ctx context.Context, org string, agent *models.Agent, tool string, input json.RawMessage, plan *proto.ChangePlan, d proto.Decision) Outcome {
	out := Outcome{Decision: d}
	if d.Effect == proto.EffectDeny || tool == proto.ToolAskUser {
		return out
	}
	now := s.now()
	windows := s.activeWindows(ctx, org, agent, now)
	for _, w := range windows {
		if w.Kind != models.WindowKindFreeze {
			continue
		}
		out.WindowID = w.ID
		if d.Effect == proto.EffectAllow && (d.Risk >= proto.RiskHigh || tool == proto.ToolChangeRun) {
			out.Decision.Effect = proto.EffectApprove
			out.Decision.Reason = fmt.Sprintf("change freeze %q: %s-risk calls need approval", w.Name, d.Risk)
		}
		return out
	}
	if d.Effect != proto.EffectApprove || d.Risk >= proto.RiskCritical || !d.Risk.Valid() {
		return out
	}
	inWindow := ""
	for _, w := range windows {
		if w.Kind == models.WindowKindWindow {
			inWindow = w.ID
			break
		}
	}
	var grants []models.ApprovalGrant
	s.db.WithContext(ctx).Where("organization_id = ? AND revoked_at IS NULL AND expires_at > ?", org, now).Order("created_at").Find(&grants)
	for i := range grants {
		g := &grants[i]
		if !g.Active(now) || !appliesTo(g.AgentID, g.Labels, agent) || !proto.GlobMatch(g.Tool, tool) {
			continue
		}
		if r, err := proto.ParseRisk(g.MaxRisk); err != nil || r >= proto.RiskCritical || d.Risk > r {
			continue
		}
		if g.Windowed && inWindow == "" {
			continue
		}
		if g.RunbookID != nil {
			if tool != proto.ToolChangeRun || plan == nil || s.matchRunbook(ctx, g, *plan) != nil {
				continue
			}
		} else if tool == proto.ToolChangeRun || !MatchArgs(g.Match, input) {
			continue
		}
		if !s.use(ctx, g, now) {
			continue
		}
		out.Decision.Effect = proto.EffectAllow
		out.Decision.Reason = fmt.Sprintf("granted by %s: %s", g.ID, g.Reason)
		out.GrantID = g.ID
		if g.Windowed {
			out.WindowID = inWindow
		}
		return out
	}
	return out
}

// use spends one use with a conditional update, so two replicas cannot both take the last one.
func (s *Service) use(ctx context.Context, g *models.ApprovalGrant, now time.Time) bool {
	res := s.db.WithContext(ctx).Model(&models.ApprovalGrant{}).
		Where("id = ? AND revoked_at IS NULL AND expires_at > ? AND (max_uses = 0 OR uses < max_uses)", g.ID, now).
		Updates(map[string]any{"uses": gorm.Expr("uses + 1"), "last_used_at": now})
	return res.Error == nil && res.RowsAffected == 1
}

func appliesTo(agentID *string, labels []string, agent *models.Agent) bool {
	if agentID != nil {
		return *agentID == agent.ID
	}
	if len(labels) == 0 {
		return false
	}
	for _, l := range labels {
		if !slices.Contains(agent.Labels, l) {
			return false
		}
	}
	return true
}

// GrantInput describes a grant; exactly one of AgentID and Labels selects the agents.
type GrantInput struct {
	AgentID        *string                      `json:"agent_id"`
	Labels         []string                     `json:"labels"`
	Tool           string                       `json:"tool"`
	Match          map[string]models.Constraint `json:"match"`
	RunbookID      *string                      `json:"runbook_id"`
	MaxRisk        string                       `json:"max_risk"`
	Windowed       bool                         `json:"windowed"`
	ExpiresInHours int                          `json:"expires_in_hours"`
	MaxUses        int                          `json:"max_uses"`
	Reason         string                       `json:"reason"`
	ApprovalID     *string                      `json:"approval_id"`
}

func (s *Service) CreateGrant(ctx context.Context, org, userID string, in GrantInput) (*models.ApprovalGrant, error) {
	set := s.Settings(ctx, org)
	in.Tool, in.Reason = strings.TrimSpace(in.Tool), strings.TrimSpace(in.Reason)
	if in.AgentID != nil && *in.AgentID == "" {
		in.AgentID = nil
	}
	switch {
	case in.Reason == "":
		return nil, errors.New("a grant needs a reason")
	case len(in.Reason) > 500:
		return nil, errors.New("the reason is too long")
	case (in.AgentID == nil) == (len(in.Labels) == 0):
		return nil, errors.New("pick one agent or a set of labels")
	case in.Tool == "" || in.Tool == proto.ToolAskUser:
		return nil, errors.New("a grant needs a tool")
	case in.ExpiresInHours < 1 || in.ExpiresInHours > set.GrantMaxHours:
		return nil, fmt.Errorf("a grant lasts between 1 and %d hours", set.GrantMaxHours)
	case in.MaxUses < 0:
		return nil, errors.New("max uses cannot be negative")
	}
	if in.AgentID != nil {
		var n int64
		s.db.WithContext(ctx).Model(&models.Agent{}).Where("id = ? AND organization_id = ?", *in.AgentID, org).Count(&n)
		if n == 0 {
			return nil, gorm.ErrRecordNotFound
		}
	}
	maxRisk, err := proto.ParseRisk(in.MaxRisk)
	if err != nil {
		return nil, err
	}
	if maxRisk >= proto.RiskCritical {
		return nil, errors.New("critical calls always need a person; a grant covers at most high risk")
	}
	if spec, ok := proto.LookupTool(in.Tool); ok && spec.Risk > maxRisk {
		return nil, fmt.Errorf("%s is %s risk, above this grant's limit", in.Tool, spec.Risk)
	}
	g := models.ApprovalGrant{Base: models.Base{ID: models.NewID("gr"), OrganizationID: org}, AgentID: in.AgentID, Labels: in.Labels,
		Tool: in.Tool, Match: in.Match, MaxRisk: maxRisk.String(), Windowed: in.Windowed, MaxUses: in.MaxUses, Reason: in.Reason,
		CreatedBy: userID, ApprovalID: in.ApprovalID, ExpiresAt: s.now().Add(time.Duration(in.ExpiresInHours) * time.Hour)}
	if g.Match == nil {
		g.Match = map[string]models.Constraint{}
	}
	for k, c := range g.Match {
		if err := ValidateConstraint(c); err != nil {
			return nil, fmt.Errorf("argument %q: %w", k, err)
		}
	}
	if in.RunbookID != nil {
		if in.Tool != proto.ToolChangeRun {
			return nil, errors.New("a runbook grant is for change_run")
		}
		var rb models.Runbook
		if err := s.db.WithContext(ctx).First(&rb, "id = ? AND organization_id = ?", *in.RunbookID, org).Error; err != nil {
			return nil, err
		}
		if !rb.Enabled {
			return nil, errors.New("the runbook is disabled")
		}
		for k := range g.Match {
			if !slices.ContainsFunc(rb.Params, func(p models.RunbookParam) bool { return p.Name == k }) {
				return nil, fmt.Errorf("the runbook has no parameter %q", k)
			}
		}
		g.RunbookID, g.RunbookHash = &rb.ID, rb.Hash
	} else if in.Tool == proto.ToolChangeRun {
		return nil, errors.New("change plans are granted through a runbook")
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&g).Error; err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{OrganizationID: org, ActorType: audit.ActorUser, ActorID: userID, Action: "grant.created",
			TargetType: "grant", TargetID: g.ID, Metadata: map[string]any{"tool": g.Tool, "agent_id": g.AgentID, "labels": g.Labels, "match": g.Match,
				"runbook_id": g.RunbookID, "max_risk": g.MaxRisk, "expires_at": g.ExpiresAt, "max_uses": g.MaxUses, "windowed": g.Windowed, "reason": g.Reason}})
	})
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// ListGrants returns the newest grants first; agentID "" lists every agent's.
func (s *Service) ListGrants(ctx context.Context, org, agentID string, activeOnly bool) ([]models.ApprovalGrant, error) {
	q := s.db.WithContext(ctx).Where("organization_id = ?", org)
	if agentID != "" {
		q = q.Where("agent_id = ?", agentID)
	}
	if activeOnly {
		q = q.Where("revoked_at IS NULL AND expires_at > ? AND (max_uses = 0 OR uses < max_uses)", s.now())
	}
	var out []models.ApprovalGrant
	return out, q.Order("created_at DESC").Limit(500).Find(&out).Error
}

func (s *Service) RevokeGrant(ctx context.Context, org, id, userID string) (*models.ApprovalGrant, error) {
	var g models.ApprovalGrant
	if err := s.db.WithContext(ctx).First(&g, "id = ? AND organization_id = ?", id, org).Error; err != nil {
		return nil, err
	}
	if g.RevokedAt != nil {
		return &g, nil
	}
	now := s.now()
	if err := s.db.WithContext(ctx).Model(&g).Updates(map[string]any{"revoked_at": now, "revoked_by": userID}).Error; err != nil {
		return nil, err
	}
	g.RevokedAt, g.RevokedBy = &now, &userID
	s.audit.Best(ctx, audit.Entry{OrganizationID: org, ActorType: audit.ActorUser, ActorID: userID, Action: "grant.revoked", TargetType: "grant", TargetID: g.ID})
	return &g, nil
}

// RevokeAll ends every active grant of the organization, e.g. when the kill switch is engaged.
func (s *Service) RevokeAll(ctx context.Context, org, by, why string) int64 {
	res := s.db.WithContext(ctx).Model(&models.ApprovalGrant{}).Where("organization_id = ? AND revoked_at IS NULL", org).
		Updates(map[string]any{"revoked_at": s.now(), "revoked_by": by})
	if res.RowsAffected > 0 {
		s.audit.Best(ctx, audit.Entry{OrganizationID: org, ActorType: audit.ActorUser, ActorID: by, Action: "grant.revoked_all",
			TargetType: "organization", TargetID: org, Metadata: map[string]any{"count": res.RowsAffected, "reason": why}})
	}
	return res.RowsAffected
}
