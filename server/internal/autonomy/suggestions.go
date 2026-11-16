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
	"crypto/sha256"
	"encoding/hex"
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
	"gorm.io/gorm"
)

const (
	SuggestGrant   = "grant"
	SuggestTighten = "tighten"
)

// suggestionWindow is how far back approval history counts as evidence.
const suggestionWindow = 30 * 24 * time.Hour

// Suggestion is evidence from approval history; only a person turns it into a grant.
type Suggestion struct {
	Key       string                       `json:"key"`
	Kind      string                       `json:"kind"`
	AgentID   string                       `json:"agent_id"`
	Tool      string                       `json:"tool"`
	Target    string                       `json:"target"`
	Approved  int                          `json:"approved"`
	Denied    int                          `json:"denied"`
	Expired   int                          `json:"expired"`
	Approvers int                          `json:"approvers"`
	FirstAt   time.Time                    `json:"first_at"`
	LastAt    time.Time                    `json:"last_at"`
	MaxRisk   string                       `json:"max_risk"`
	Match     map[string]models.Constraint `json:"match,omitempty"`
	Text      string                       `json:"text"`
}

type group struct {
	tool, target string
	approved     []models.Approval
	denied       int
	expired      int
	first, last  time.Time
	maxRisk      proto.Risk
}

func target(tool string, input json.RawMessage) string {
	spec, ok := proto.LookupTool(tool)
	if ok {
		if r, err := spec.Resources(input); err == nil {
			var parts []string
			for _, l := range []struct {
				name string
				v    []string
			}{{"apps", r.Apps}, {"services", r.Services}, {"containers", r.Containers}, {"paths", r.Paths}, {"domains", r.Domains}, {"commands", r.Commands}} {
				if len(l.v) > 0 {
					parts = append(parts, l.name+": "+strings.Join(l.v, ", "))
				}
			}
			if len(parts) > 0 {
				return strings.Join(parts, "; ")
			}
		}
	}
	var v any
	_ = json.Unmarshal(orEmpty(input), &v)
	return canonical(v)
}

// Suggestions reads the agent's recent approvals; nothing here changes any permission.
func (s *Service) Suggestions(ctx context.Context, org, agentID string) ([]Suggestion, error) {
	set := s.Settings(ctx, org)
	now := s.now()
	var aps []models.Approval
	err := s.db.WithContext(ctx).Where("organization_id = ? AND agent_id = ? AND created_at > ? AND status <> ? AND tool NOT IN ?",
		org, agentID, now.Add(-suggestionWindow), models.ApprovalPending, []string{proto.ToolChangeRun, proto.ToolAskUser}).
		Order("created_at").Limit(5000).Find(&aps).Error
	if err != nil {
		return nil, err
	}
	groups := map[string]*group{}
	var order []string
	for _, ap := range aps {
		risk, _ := proto.ParseRisk(ap.Risk)
		if risk >= proto.RiskCritical {
			continue
		}
		tg := target(ap.Tool, ap.Input)
		k := ap.Tool + "\n" + tg
		g := groups[k]
		if g == nil {
			g = &group{tool: ap.Tool, target: tg, first: ap.CreatedAt}
			groups[k] = g
			order = append(order, k)
		}
		g.last = ap.CreatedAt
		g.maxRisk = max(g.maxRisk, risk)
		switch {
		case ap.Status == models.ApprovalApproved && ap.DecidedBy != nil:
			g.approved = append(g.approved, ap)
		case ap.Status == models.ApprovalDenied:
			g.denied++
		case ap.Status == models.ApprovalExpired:
			g.expired++
		}
	}
	var dismissed []models.SuggestionDismissal
	s.db.WithContext(ctx).Where("organization_id = ? AND agent_id = ?", org, agentID).Find(&dismissed)
	grants, _ := s.ListGrants(ctx, org, agentID, true)

	var out []Suggestion
	for _, k := range order {
		g := groups[k]
		sg := Suggestion{AgentID: agentID, Tool: g.tool, Target: g.target, Approved: len(g.approved), Denied: g.denied, Expired: g.expired,
			FirstAt: g.first, LastAt: g.last, MaxRisk: g.maxRisk.String()}
		approvers := map[string]bool{}
		for _, ap := range g.approved {
			approvers[*ap.DecidedBy] = true
		}
		sg.Approvers = len(approvers)
		switch {
		case g.denied >= 3 && g.denied > len(g.approved):
			sg.Kind = SuggestTighten
			sg.Text = fmt.Sprintf("Declined %d times in 30 days. Consider denying %s for this target in the agent's policy.", g.denied, g.tool)
		case len(g.approved) >= set.SuggestMinApprovals && g.denied == 0 && sg.Approvers >= set.SuggestMinApprovers &&
			g.last.Sub(g.first) >= time.Duration(set.SuggestMinDays)*24*time.Hour:
			m, ok := inferMatch(g.approved)
			if !ok || coveredBy(grants, g.tool, g.approved[len(g.approved)-1].Input) {
				continue
			}
			sg.Kind, sg.Match = SuggestGrant, m
			sg.Text = fmt.Sprintf("Approved %d times by %d people over %d days, never declined. A grant would let it run without asking.",
				len(g.approved), sg.Approvers, int(g.last.Sub(g.first).Hours()/24))
		default:
			continue
		}
		sum := sha256.Sum256([]byte(agentID + "\n" + sg.Kind + "\n" + k))
		sg.Key = hex.EncodeToString(sum[:16])
		// A dismissal holds until there is newer evidence.
		if slices.ContainsFunc(dismissed, func(d models.SuggestionDismissal) bool { return d.Key == sg.Key && !g.last.After(d.CreatedAt) }) {
			continue
		}
		out = append(out, sg)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].LastAt.After(out[j].LastAt) })
	return out, nil
}

// inferMatch keeps every argument as narrow as the evidence: one value, the values seen, or the range seen.
func inferMatch(aps []models.Approval) (map[string]models.Constraint, bool) {
	var keys []string
	vals := map[string][]any{}
	for i, ap := range aps {
		args := map[string]any{}
		if err := json.Unmarshal(orEmpty(ap.Input), &args); err != nil {
			return nil, false
		}
		var ks []string
		for k := range args {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		if i == 0 {
			keys = ks
		} else if !slices.Equal(keys, ks) {
			return nil, false
		}
		for k, v := range args {
			vals[k] = append(vals[k], v)
		}
	}
	m := map[string]models.Constraint{}
	for _, k := range keys {
		vs := vals[k]
		distinct := map[string]bool{}
		allStrings, allNumbers := true, true
		for _, v := range vs {
			distinct[canonical(v)] = true
			_, isS := v.(string)
			_, isN := v.(float64)
			allStrings, allNumbers = allStrings && isS, allNumbers && isN
		}
		switch {
		case len(distinct) == 1:
			m[k] = models.Constraint{Equals: json.RawMessage(canonical(vs[0]))}
		case allStrings && len(distinct) <= 5:
			var one []string
			for _, v := range vs {
				if s := v.(string); !slices.Contains(one, s) {
					one = append(one, s)
				}
			}
			sort.Strings(one)
			m[k] = models.Constraint{OneOf: one}
		case allNumbers:
			lo, hi := vs[0].(float64), vs[0].(float64)
			for _, v := range vs {
				lo, hi = min(lo, v.(float64)), max(hi, v.(float64))
			}
			m[k] = models.Constraint{Min: &lo, Max: &hi}
		default:
			return nil, false
		}
	}
	return m, true
}

func coveredBy(grants []models.ApprovalGrant, tool string, input json.RawMessage) bool {
	return slices.ContainsFunc(grants, func(g models.ApprovalGrant) bool {
		return g.RunbookID == nil && proto.GlobMatch(g.Tool, tool) && MatchArgs(g.Match, input)
	})
}

func (s *Service) find(ctx context.Context, org, agentID, key string) (*Suggestion, error) {
	all, err := s.Suggestions(ctx, org, agentID)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].Key == key {
			return &all[i], nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

// AcceptSuggestion creates the suggested grant through the normal, audited path.
func (s *Service) AcceptSuggestion(ctx context.Context, org, userID, agentID, key string, hours, maxUses int, reason string) (*models.ApprovalGrant, error) {
	sg, err := s.find(ctx, org, agentID, key)
	if err != nil {
		return nil, err
	}
	if sg.Kind != SuggestGrant {
		return nil, errors.New("only grant suggestions can be accepted; change the policy for the others")
	}
	if strings.TrimSpace(reason) == "" {
		reason = fmt.Sprintf("suggested from %d approvals by %d people", sg.Approved, sg.Approvers)
	}
	return s.CreateGrant(ctx, org, userID, GrantInput{AgentID: &agentID, Tool: sg.Tool, Match: sg.Match, MaxRisk: sg.MaxRisk,
		ExpiresInHours: hours, MaxUses: maxUses, Reason: reason})
}

func (s *Service) DismissSuggestion(ctx context.Context, org, userID, agentID, key string) error {
	if _, err := s.find(ctx, org, agentID, key); err != nil {
		return err
	}
	d := models.SuggestionDismissal{Base: models.Base{ID: models.NewID("sgd"), OrganizationID: org}, AgentID: agentID, Key: key, By: userID}
	if err := s.db.WithContext(ctx).Create(&d).Error; err != nil {
		return err
	}
	s.audit.Best(ctx, audit.Entry{OrganizationID: org, ActorType: audit.ActorUser, ActorID: userID, Action: "suggestion.dismissed",
		TargetType: "agent", TargetID: agentID, Metadata: map[string]any{"key": key}})
	return nil
}
