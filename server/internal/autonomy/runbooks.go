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

package autonomy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/audit"
	"github.com/goakili/akili/server/internal/models"
	"gorm.io/gorm"
)

const maxRunbookCalls = 20

// RunbookInput is a change plan template whose inputs may use "{{name}}" placeholders, each declared in Params.
type RunbookInput struct {
	Name           string                `json:"name"`
	Description    string                `json:"description"`
	Plan           json.RawMessage       `json:"plan"`
	Params         []models.RunbookParam `json:"params"`
	Enabled        bool                  `json:"enabled"`
	SourceChangeID *string               `json:"source_change_id"`
}

func parseTemplate(raw json.RawMessage) (proto.ChangePlan, error) {
	var p proto.ChangePlan
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return p, fmt.Errorf("invalid plan: %w", err)
	}
	return p, nil
}

func (in *RunbookInput) validate() error {
	in.Name, in.Description = strings.TrimSpace(in.Name), strings.TrimSpace(in.Description)
	if in.Name == "" {
		return errors.New("a runbook needs a name")
	}
	p, err := parseTemplate(in.Plan)
	if err != nil {
		return err
	}
	switch {
	case len(p.Steps) == 0:
		return errors.New("a runbook needs at least one step")
	case len(p.Verify) == 0:
		return errors.New("a runbook needs at least one verify check, so failures roll back")
	case len(p.Steps)+len(p.Verify)+len(p.Rollback) > maxRunbookCalls:
		return fmt.Errorf("a runbook can have at most %d calls", maxRunbookCalls)
	}
	for phase, calls := range map[string][]proto.ChangeCall{proto.PhaseStep: p.Steps, proto.PhaseVerify: p.Verify, proto.PhaseRollback: p.Rollback} {
		for i, c := range calls {
			spec, ok := proto.LookupTool(c.Tool)
			switch {
			case !ok:
				return fmt.Errorf("%s %d: unknown tool %q", phase, i+1, c.Tool)
			case c.Tool == proto.ToolChangeRun || c.Tool == proto.ToolAskUser:
				return fmt.Errorf("%s %d: %s cannot be part of a runbook", phase, i+1, c.Tool)
			case phase == proto.PhaseVerify && spec.Risk > proto.RiskLow:
				return fmt.Errorf("verify %d: checks must be low-risk tools; %s is %s risk", i+1, c.Tool, spec.Risk)
			case spec.Risk >= proto.RiskCritical:
				return fmt.Errorf("%s %d: %s is critical and always needs a person", phase, i+1, c.Tool)
			}
		}
	}
	used := Placeholders(in.Plan)
	for _, prm := range in.Params {
		if !slices.Contains(used, prm.Name) {
			return fmt.Errorf("parameter %q is not used in the plan", prm.Name)
		}
		if err := ValidateConstraint(prm.Bounds); err != nil {
			return fmt.Errorf("parameter %q: %w", prm.Name, err)
		}
	}
	for _, n := range used {
		if !slices.ContainsFunc(in.Params, func(p models.RunbookParam) bool { return p.Name == n }) {
			return fmt.Errorf("placeholder {{%s}} has no parameter with bounds", n)
		}
	}
	return nil
}

// runbookHash covers the calls and the bounds; the title, reason and description are free text.
func runbookHash(plan json.RawMessage, params []models.RunbookParam) string {
	p, _ := parseTemplate(plan)
	b, _ := json.Marshal(struct {
		Steps, Verify, Rollback []proto.ChangeCall
		Params                  []models.RunbookParam
	}{p.Steps, p.Verify, p.Rollback, params})
	var buf bytes.Buffer
	_ = json.Compact(&buf, b)
	sum := sha256.Sum256(buf.Bytes())
	return hex.EncodeToString(sum[:])
}

func (s *Service) ListRunbooks(ctx context.Context, org string) ([]models.Runbook, error) {
	var out []models.Runbook
	return out, s.db.WithContext(ctx).Where("organization_id = ?", org).Order("name").Find(&out).Error
}

func (s *Service) GetRunbook(ctx context.Context, org, id string) (*models.Runbook, error) {
	var rb models.Runbook
	return &rb, s.db.WithContext(ctx).First(&rb, "id = ? AND organization_id = ?", id, org).Error
}

// SaveRunbook creates or edits a runbook; an edit that changes the calls or bounds bumps the version and hash.
func (s *Service) SaveRunbook(ctx context.Context, org, userID, id string, in RunbookInput) (*models.Runbook, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	_ = json.Compact(&buf, in.Plan)
	rb := models.Runbook{Base: models.Base{ID: models.NewID("rb"), OrganizationID: org}, CreatedBy: userID, Version: 1, SourceChangeID: in.SourceChangeID}
	if id != "" {
		rb = models.Runbook{}
		if err := s.db.WithContext(ctx).First(&rb, "id = ? AND organization_id = ?", id, org).Error; err != nil {
			return nil, err
		}
	}
	hash := runbookHash(buf.Bytes(), in.Params)
	if id != "" && hash != rb.Hash {
		rb.Version++
	}
	rb.Name, rb.Description, rb.Plan, rb.Params, rb.Hash, rb.Enabled = in.Name, in.Description, buf.Bytes(), in.Params, hash, in.Enabled
	if rb.Params == nil {
		rb.Params = []models.RunbookParam{}
	}
	if err := s.db.WithContext(ctx).Save(&rb).Error; err != nil {
		return nil, err
	}
	s.audit.Best(ctx, audit.Entry{OrganizationID: org, ActorType: audit.ActorUser, ActorID: userID, Action: "runbook.saved",
		TargetType: "runbook", TargetID: rb.ID, Metadata: map[string]any{"name": rb.Name, "version": rb.Version, "hash": rb.Hash, "enabled": rb.Enabled}})
	return &rb, nil
}

// DeleteRunbook also revokes the grants bound to it.
func (s *Service) DeleteRunbook(ctx context.Context, org, userID, id string) error {
	res := s.db.WithContext(ctx).Where("id = ? AND organization_id = ?", id, org).Delete(&models.Runbook{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	s.db.WithContext(ctx).Model(&models.ApprovalGrant{}).Where("runbook_id = ? AND revoked_at IS NULL", id).
		Updates(map[string]any{"revoked_at": s.now(), "revoked_by": userID})
	s.audit.Best(ctx, audit.Entry{OrganizationID: org, ActorType: audit.ActorUser, ActorID: userID, Action: "runbook.deleted", TargetType: "runbook", TargetID: id})
	return nil
}

// DraftFromChange turns a recorded change into a runbook plan without placeholders, for a person to generalise.
func (s *Service) DraftFromChange(ctx context.Context, org, changeID string) (*RunbookInput, error) {
	var ch models.Change
	if err := s.db.WithContext(ctx).First(&ch, "id = ? AND organization_id = ?", changeID, org).Error; err != nil {
		return nil, err
	}
	p := proto.ChangePlan{Title: ch.Title, Reason: ch.Reason}
	for _, c := range ch.Calls {
		cc := proto.ChangeCall{Tool: c.Tool, Input: c.Input, Description: c.Description, Expect: c.Expect, Reject: c.Reject}
		switch c.Phase {
		case proto.PhaseStep:
			p.Steps = append(p.Steps, cc)
		case proto.PhaseVerify:
			p.Verify = append(p.Verify, cc)
		case proto.PhaseRollback:
			p.Rollback = append(p.Rollback, cc)
		}
	}
	plan, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return nil, err
	}
	return &RunbookInput{Name: ch.Title, Plan: plan, Params: []models.RunbookParam{}, SourceChangeID: &ch.ID}, nil
}

// matchRunbook checks a proposed plan against the grant's runbook version, the runbook's bounds and the grant's own narrower bounds.
func (s *Service) matchRunbook(ctx context.Context, g *models.ApprovalGrant, plan proto.ChangePlan) error {
	var rb models.Runbook
	if err := s.db.WithContext(ctx).First(&rb, "id = ? AND organization_id = ?", *g.RunbookID, g.OrganizationID).Error; err != nil {
		return err
	}
	if !rb.Enabled || rb.Hash != g.RunbookHash {
		return errors.New("the runbook changed or is disabled")
	}
	tmpl, err := parseTemplate(rb.Plan)
	if err != nil {
		return err
	}
	b, err := MatchPlan(tmpl, plan)
	if err != nil {
		return err
	}
	for _, p := range rb.Params {
		v, ok := b[p.Name]
		if !ok || !Satisfies(p.Bounds, v) {
			return fmt.Errorf("parameter %q is outside the runbook's bounds", p.Name)
		}
		if c, ok := g.Match[p.Name]; ok && !Satisfies(c, v) {
			return fmt.Errorf("parameter %q is outside the grant's bounds", p.Name)
		}
	}
	return nil
}

// Hint tells a triage agent that a runbook exists and the exact plan shape a grant would cover.
func (s *Service) Hint(ctx context.Context, org, id string) string {
	rb, err := s.GetRunbook(ctx, org, id)
	if err != nil || !rb.Enabled {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Runbook %q fits this alert. If it is the right fix, propose it with change_run using exactly this plan, "+
		"filling each {{placeholder}} (title and reason are yours to write):\n%s\n", rb.Name, rb.Plan)
	for _, p := range rb.Params {
		bounds, _ := json.Marshal(p.Bounds)
		fmt.Fprintf(&b, "- %s: %s %s\n", p.Name, p.Description, bounds)
	}
	return b.String()
}
