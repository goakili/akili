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
	"strings"
	"testing"
	"time"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/audit"
	"github.com/goakili/akili/server/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type memAudit struct{ actions []string }

func (m *memAudit) Record(_ context.Context, e audit.Entry) error {
	m.actions = append(m.actions, e.Action)
	return nil
}
func (m *memAudit) Best(_ context.Context, e audit.Entry) { m.actions = append(m.actions, e.Action) }

const org = "org_1"

func newService(t *testing.T) (*Service, *gorm.DB, *models.Agent) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Organization{}, &models.Agent{}, &models.Approval{}, &models.Change{}, &models.ApprovalGrant{},
		&models.ChangeWindow{}, &models.Runbook{}, &models.SuggestionDismissal{}); err != nil {
		t.Fatal(err)
	}
	db.Create(&models.Organization{ID: org, Name: "o", Slug: "o"})
	ag := &models.Agent{Base: models.Base{ID: "ag_1", OrganizationID: org}, Name: "ops", Status: "online", Labels: []string{"prod", "eu"}}
	db.Create(ag)
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	return &Service{db: db, audit: &memAudit{}, now: func() time.Time { return now }}, db, ag
}

func raw(s string) json.RawMessage { return json.RawMessage(s) }

func approve(r proto.Risk) proto.Decision {
	return proto.Decision{Effect: proto.EffectApprove, Risk: r, Reason: "needs approval"}
}

func grant(t *testing.T, s *Service, in GrantInput) *models.ApprovalGrant {
	t.Helper()
	if in.Reason == "" {
		in.Reason = "routine restarts"
	}
	if in.ExpiresInHours == 0 {
		in.ExpiresInHours = 2
	}
	if in.MaxRisk == "" {
		in.MaxRisk = "high"
	}
	g, err := s.CreateGrant(context.Background(), org, "usr_1", in)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func eq(v string) models.Constraint { return models.Constraint{Equals: raw(v)} }

var restartStaging = raw(`{"workspace":"staging","app":"api"}`)

func restartGrant(agent string) GrantInput {
	return GrantInput{AgentID: &agent, Tool: proto.ToolMiabiRestart, Match: map[string]models.Constraint{"workspace": eq(`"staging"`), "app": {Glob: "api*"}}}
}

func TestGrantTurnsApproveIntoAllow(t *testing.T) {
	s, db, ag := newService(t)
	g := grant(t, s, restartGrant(ag.ID))
	out := s.Review(context.Background(), org, ag, proto.ToolMiabiRestart, restartStaging, nil, approve(proto.RiskHigh))
	if out.Decision.Effect != proto.EffectAllow || out.GrantID != g.ID || !strings.Contains(out.Decision.Reason, g.ID) {
		t.Fatalf("got %+v", out)
	}
	db.First(g, "id = ?", g.ID)
	if g.Uses != 1 || g.LastUsedAt == nil {
		t.Fatalf("use not counted: %+v", g)
	}
}

func TestGrantNeverLiftsADenial(t *testing.T) {
	s, _, ag := newService(t)
	grant(t, s, restartGrant(ag.ID))
	deny := proto.Decision{Effect: proto.EffectDeny, Risk: proto.RiskHigh, Reason: "policy says no"}
	if out := s.Review(context.Background(), org, ag, proto.ToolMiabiRestart, restartStaging, nil, deny); out.Decision.Effect != proto.EffectDeny || out.Decision.Reason != deny.Reason || out.GrantID != "" {
		t.Fatalf("a denial changed: %+v", out)
	}
}

func TestGrantNeverCoversCritical(t *testing.T) {
	s, db, ag := newService(t)
	if _, err := s.CreateGrant(context.Background(), org, "usr_1", GrantInput{AgentID: &ag.ID, Tool: proto.ToolMiabiDBRestore, MaxRisk: "critical",
		ExpiresInHours: 1, Reason: "x"}); err == nil {
		t.Fatal("a critical grant was accepted")
	}
	if _, err := s.CreateGrant(context.Background(), org, "usr_1", GrantInput{AgentID: &ag.ID, Tool: proto.ToolMiabiDBRestore, MaxRisk: "high",
		ExpiresInHours: 1, Reason: "x"}); err == nil {
		t.Fatal("a grant for a critical tool was accepted")
	}
	// A wildcard grant stored directly must still not cover a critical call.
	db.Create(&models.ApprovalGrant{Base: models.Base{ID: "gr_x", OrganizationID: org}, AgentID: &ag.ID, Tool: "*", MaxRisk: "critical",
		Match: map[string]models.Constraint{"workspace": {Glob: "*"}, "database": {Glob: "*"}, "backup": {Glob: "*"}}, ExpiresAt: s.now().Add(time.Hour), Reason: "x"})
	in := raw(`{"workspace":"prod","database":"main","backup":"b1"}`)
	if out := s.Review(context.Background(), org, ag, proto.ToolMiabiDBRestore, in, nil, approve(proto.RiskCritical)); out.Decision.Effect != proto.EffectApprove {
		t.Fatalf("critical call allowed: %+v", out)
	}
}

func TestGrantBounds(t *testing.T) {
	s, _, ag := newService(t)
	other := "ag_2"
	ctx := context.Background()
	in := restartGrant(ag.ID)
	in.MaxUses = 1
	grant(t, s, in)
	cases := map[string]struct {
		agent *models.Agent
		input json.RawMessage
	}{
		"other workspace":  {ag, raw(`{"workspace":"prod","app":"api"}`)},
		"extra argument":   {ag, raw(`{"workspace":"staging","app":"api","integration":"x"}`)},
		"missing argument": {ag, raw(`{"workspace":"staging"}`)},
		"other agent":      {&models.Agent{Base: models.Base{ID: other}}, restartStaging},
	}
	for name, c := range cases {
		if out := s.Review(ctx, org, c.agent, proto.ToolMiabiRestart, c.input, nil, approve(proto.RiskHigh)); out.Decision.Effect != proto.EffectApprove {
			t.Errorf("%s: allowed", name)
		}
	}
	if out := s.Review(ctx, org, ag, proto.ToolMiabiRestart, restartStaging, nil, approve(proto.RiskHigh)); out.Decision.Effect != proto.EffectAllow {
		t.Fatal("first use refused")
	}
	if out := s.Review(ctx, org, ag, proto.ToolMiabiRestart, restartStaging, nil, approve(proto.RiskHigh)); out.Decision.Effect != proto.EffectApprove {
		t.Fatal("used past max_uses")
	}
}

func TestGrantExpiresAndRevokes(t *testing.T) {
	s, _, ag := newService(t)
	ctx := context.Background()
	g := grant(t, s, restartGrant(ag.ID))
	later := s.now().Add(3 * time.Hour)
	s.now = func() time.Time { return later }
	if out := s.Review(ctx, org, ag, proto.ToolMiabiRestart, restartStaging, nil, approve(proto.RiskHigh)); out.Decision.Effect != proto.EffectApprove {
		t.Fatal("expired grant used")
	}
	s.now = func() time.Time { return g.CreatedAt }
	g2 := grant(t, s, restartGrant(ag.ID))
	if n := s.RevokeAll(ctx, org, "usr_1", "kill switch"); n != 2 {
		t.Fatalf("revoked %d, want 2", n)
	}
	if out := s.Review(ctx, org, ag, proto.ToolMiabiRestart, restartStaging, nil, approve(proto.RiskHigh)); out.Decision.Effect != proto.EffectApprove {
		t.Fatalf("revoked grant %s used", g2.ID)
	}
}

func TestCreateGrantValidation(t *testing.T) {
	s, _, ag := newService(t)
	bad := map[string]GrantInput{
		"no reason":       {AgentID: &ag.ID, Tool: proto.ToolMiabiRestart, MaxRisk: "high", ExpiresInHours: 1},
		"too long":        {AgentID: &ag.ID, Tool: proto.ToolMiabiRestart, MaxRisk: "high", ExpiresInHours: 25, Reason: "x"},
		"no selector":     {Tool: proto.ToolMiabiRestart, MaxRisk: "high", ExpiresInHours: 1, Reason: "x"},
		"risk too low":    {AgentID: &ag.ID, Tool: proto.ToolMiabiRestart, MaxRisk: "medium", ExpiresInHours: 1, Reason: "x"},
		"ask_user":        {AgentID: &ag.ID, Tool: proto.ToolAskUser, MaxRisk: "low", ExpiresInHours: 1, Reason: "x"},
		"bare change_run": {AgentID: &ag.ID, Tool: proto.ToolChangeRun, MaxRisk: "high", ExpiresInHours: 1, Reason: "x"},
		"bad constraint":  {AgentID: &ag.ID, Tool: proto.ToolMiabiRestart, MaxRisk: "high", ExpiresInHours: 1, Reason: "x", Match: map[string]models.Constraint{"app": {}}},
		"foreign agent":   {AgentID: new(string), Tool: proto.ToolMiabiRestart, MaxRisk: "high", ExpiresInHours: 1, Reason: "x"},
	}
	*bad["foreign agent"].AgentID = "ag_other"
	for name, in := range bad {
		if _, err := s.CreateGrant(context.Background(), org, "usr_1", in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestLabelGrant(t *testing.T) {
	s, _, ag := newService(t)
	in := restartGrant("")
	in.AgentID, in.Labels = nil, []string{"prod"}
	grant(t, s, in)
	if out := s.Review(context.Background(), org, ag, proto.ToolMiabiRestart, restartStaging, nil, approve(proto.RiskHigh)); out.Decision.Effect != proto.EffectAllow {
		t.Fatal("label grant not applied")
	}
	stranger := &models.Agent{Base: models.Base{ID: "ag_3"}, Labels: []string{"dev"}}
	if out := s.Review(context.Background(), org, stranger, proto.ToolMiabiRestart, restartStaging, nil, approve(proto.RiskHigh)); out.Decision.Effect != proto.EffectApprove {
		t.Fatal("label grant covered an agent without the label")
	}
}

func TestFreezeForcesApprovalAndPausesGrants(t *testing.T) {
	s, _, ag := newService(t)
	ctx := context.Background()
	grant(t, s, restartGrant(ag.ID))
	start, end := s.now().Add(-time.Hour), s.now().Add(time.Hour)
	if _, err := s.SaveWindow(ctx, org, "usr_1", "", WindowInput{Name: "Black Friday", Kind: models.WindowKindFreeze, StartsAt: &start, EndsAt: &end, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	allowHigh := proto.Decision{Effect: proto.EffectAllow, Risk: proto.RiskHigh}
	if out := s.Review(ctx, org, ag, proto.ToolMiabiRestart, restartStaging, nil, allowHigh); out.Decision.Effect != proto.EffectApprove || out.WindowID == "" {
		t.Fatalf("freeze did not force approval: %+v", out)
	}
	allowLow := proto.Decision{Effect: proto.EffectAllow, Risk: proto.RiskLow}
	if out := s.Review(ctx, org, ag, proto.ToolMiabiStatus, restartStaging, nil, allowLow); out.Decision.Effect != proto.EffectAllow {
		t.Fatal("freeze blocked a low-risk read")
	}
	if out := s.Review(ctx, org, ag, proto.ToolMiabiRestart, restartStaging, nil, approve(proto.RiskHigh)); out.Decision.Effect != proto.EffectApprove {
		t.Fatal("a grant applied during a freeze")
	}
}

func TestWindowedGrant(t *testing.T) {
	s, _, ag := newService(t)
	ctx := context.Background()
	in := restartGrant(ag.ID)
	in.Windowed = true
	grant(t, s, in)
	if out := s.Review(ctx, org, ag, proto.ToolMiabiRestart, restartStaging, nil, approve(proto.RiskHigh)); out.Decision.Effect != proto.EffectApprove {
		t.Fatal("windowed grant used outside a window")
	}
	// Every day 11:30-13:30 Europe/Berlin is 10:30-12:30 UTC, which covers 12:00 UTC.
	if _, err := s.SaveWindow(ctx, org, "usr_1", "", WindowInput{Name: "daily", Kind: models.WindowKindWindow, Cron: "30 11 * * *", DurationMin: 120,
		Timezone: "Europe/Berlin", Labels: []string{"eu"}, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if out := s.Review(ctx, org, ag, proto.ToolMiabiRestart, restartStaging, nil, approve(proto.RiskHigh)); out.Decision.Effect != proto.EffectAllow || out.WindowID == "" {
		t.Fatalf("windowed grant not used inside its window: %+v", out)
	}
}

func TestActiveAt(t *testing.T) {
	at := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		w    models.ChangeWindow
		want bool
	}{
		{models.ChangeWindow{Cron: "0 12 * * *", DurationMin: 1, Enabled: true}, true},
		{models.ChangeWindow{Cron: "1 12 * * *", DurationMin: 60, Enabled: true}, false},
		{models.ChangeWindow{Cron: "0 11 * * *", DurationMin: 60, Enabled: true}, false},
		{models.ChangeWindow{Cron: "0 11 * * *", DurationMin: 61, Enabled: true}, true},
		{models.ChangeWindow{Cron: "0 13 * * *", DurationMin: 60, Timezone: "Europe/Berlin", Enabled: true}, true},
		{models.ChangeWindow{Cron: "0 12 * * *", DurationMin: 60}, false},
	}
	for i, c := range cases {
		if got := ActiveAt(&c.w, at); got != c.want {
			t.Errorf("%d: ActiveAt = %v, want %v", i, got, c.want)
		}
	}
}

func TestConstraints(t *testing.T) {
	lo, hi := 1.0, 5.0
	cases := []struct {
		c    models.Constraint
		v    any
		want bool
	}{
		{eq(`"a"`), "a", true},
		{eq(`3`), 3.0, true},
		{eq(`3`), "3", false},
		{models.Constraint{Glob: "api-*"}, "api-eu", true},
		{models.Constraint{Glob: "api-*"}, 3.0, false},
		{models.Constraint{OneOf: []string{"a", "2"}}, 2.0, true},
		{models.Constraint{OneOf: []string{"a"}}, "b", false},
		{models.Constraint{Min: &lo, Max: &hi}, 5.0, true},
		{models.Constraint{Min: &lo, Max: &hi}, 6.0, false},
		{models.Constraint{Min: &lo}, "3", false},
	}
	for i, c := range cases {
		if got := Satisfies(c.c, c.v); got != c.want {
			t.Errorf("%d: Satisfies = %v, want %v", i, got, c.want)
		}
	}
	if ValidateConstraint(models.Constraint{Glob: "x", OneOf: []string{"y"}}) == nil || ValidateConstraint(models.Constraint{Min: &hi, Max: &lo}) == nil {
		t.Fatal("invalid constraints accepted")
	}
}

const scalePlan = `{"title":"t","reason":"r",
 "steps":[{"tool":"miabi_scale","input":{"workspace":"{{ws}}","app":"api","replicas":"{{replicas}}"}}],
 "verify":[{"tool":"miabi_status","input":{"workspace":"{{ws}}","app":"api"},"expect":"healthy"}]}`

func runbook(t *testing.T, s *Service) *models.Runbook {
	t.Helper()
	lo, hi := 1.0, 6.0
	rb, err := s.SaveRunbook(context.Background(), org, "usr_1", "", RunbookInput{Name: "scale api", Plan: raw(scalePlan), Enabled: true,
		Params: []models.RunbookParam{{Name: "ws", Bounds: models.Constraint{OneOf: []string{"staging", "prod"}}}, {Name: "replicas", Bounds: models.Constraint{Min: &lo, Max: &hi}}}})
	if err != nil {
		t.Fatal(err)
	}
	return rb
}

func plan(t *testing.T, ws string, replicas int) *proto.ChangePlan {
	t.Helper()
	p, err := proto.ParseChangePlan(raw(`{"title":"Scale up","reason":"latency",
 "steps":[{"tool":"miabi_scale","input":{"workspace":"` + ws + `","app":"api","replicas":` + itoa(replicas) + `}}],
 "verify":[{"tool":"miabi_status","input":{"workspace":"` + ws + `","app":"api"},"expect":"healthy"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	return &p
}

func itoa(i int) string { b, _ := json.Marshal(i); return string(b) }

func TestRunbookGrant(t *testing.T) {
	s, _, ag := newService(t)
	ctx := context.Background()
	rb := runbook(t, s)
	hi := 4.0
	grant(t, s, GrantInput{AgentID: &ag.ID, Tool: proto.ToolChangeRun, RunbookID: &rb.ID, Match: map[string]models.Constraint{"replicas": {Max: &hi}}})
	review := func(p *proto.ChangePlan) string {
		return s.Review(ctx, org, ag, proto.ToolChangeRun, nil, p, approve(proto.RiskHigh)).Decision.Effect
	}
	if got := review(plan(t, "staging", 3)); got != proto.EffectAllow {
		t.Fatalf("plan within bounds: %s", got)
	}
	if got := review(plan(t, "staging", 5)); got != proto.EffectApprove {
		t.Fatal("plan past the grant's own bound was allowed")
	}
	if got := review(plan(t, "dev", 2)); got != proto.EffectApprove {
		t.Fatal("plan past the runbook's bound was allowed")
	}
	other := plan(t, "staging", 2)
	other.Steps[0].Input = raw(`{"workspace":"staging","app":"web","replicas":2}`)
	if got := review(other); got != proto.EffectApprove {
		t.Fatal("plan for another app was allowed")
	}
	in := RunbookInput{Name: rb.Name, Plan: rb.Plan, Params: rb.Params, Enabled: true}
	in.Params[1].Bounds.Max = &hi
	if _, err := s.SaveRunbook(ctx, org, "usr_1", rb.ID, in); err != nil {
		t.Fatal(err)
	}
	if got := review(plan(t, "staging", 2)); got != proto.EffectApprove {
		t.Fatal("a grant survived an edit of its runbook")
	}
}

func TestRunbookValidation(t *testing.T) {
	s, _, _ := newService(t)
	bad := map[string]RunbookInput{
		"undeclared placeholder": {Name: "x", Plan: raw(scalePlan)},
		"no verify":              {Name: "x", Plan: raw(`{"steps":[{"tool":"miabi_restart","input":{"workspace":"a","app":"b"}}],"verify":[]}`)},
		"critical step": {Name: "x", Plan: raw(`{"steps":[{"tool":"miabi_db_restore","input":{}}],
			"verify":[{"tool":"miabi_status","input":{"workspace":"a","app":"b"}}]}`)},
		"risky verify": {Name: "x", Plan: raw(`{"steps":[{"tool":"miabi_restart","input":{"workspace":"a","app":"b"}}],
			"verify":[{"tool":"miabi_restart","input":{"workspace":"a","app":"b"}}]}`)},
	}
	for name, in := range bad {
		if _, err := s.SaveRunbook(context.Background(), org, "usr_1", "", in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestMatchPlanBindsPartsOfStrings(t *testing.T) {
	tmpl, _ := parseTemplate(raw(`{"steps":[{"tool":"shell","input":{"command":"systemctl restart {{unit}}.service"}},
		{"tool":"shell","input":{"command":"journalctl -u {{unit}}.service"}}],"verify":[]}`))
	p := proto.ChangePlan{Steps: []proto.ChangeCall{{Tool: "shell", Input: raw(`{"command":"systemctl restart nginx.service"}`)},
		{Tool: "shell", Input: raw(`{"command":"journalctl -u nginx.service"}`)}}}
	b, err := MatchPlan(tmpl, p)
	if err != nil || b["unit"] != "nginx" {
		t.Fatalf("bindings %v, err %v", b, err)
	}
	p.Steps[1].Input = raw(`{"command":"journalctl -u sshd.service"}`)
	if _, err := MatchPlan(tmpl, p); err == nil {
		t.Fatal("one parameter took two values")
	}
}

func seedApprovals(db *gorm.DB, ag *models.Agent, n int, status string, input string, start time.Time, approvers ...string) {
	for i := range n {
		var by *string
		if len(approvers) > 0 {
			by = &approvers[i%len(approvers)]
		}
		db.Create(&models.Approval{Base: models.Base{ID: models.NewID("apr"), OrganizationID: org, CreatedAt: start.Add(time.Duration(i) * 24 * time.Hour)},
			SessionID: "ses_1", AgentID: ag.ID, RequestID: "r", Tool: proto.ToolMiabiRestart, Input: raw(input), InputHash: "h",
			Risk: "high", Status: status, DecidedBy: by})
	}
}

func TestSuggestionFromApprovals(t *testing.T) {
	s, db, ag := newService(t)
	ctx := context.Background()
	start := s.now().Add(-20 * 24 * time.Hour)
	seedApprovals(db, ag, 12, models.ApprovalApproved, `{"workspace":"staging","app":"api"}`, start, "usr_1", "usr_2")
	seedApprovals(db, ag, 9, models.ApprovalApproved, `{"workspace":"prod","app":"api"}`, start, "usr_1", "usr_2")
	sg, err := s.Suggestions(ctx, org, ag.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sg) != 1 || sg[0].Kind != SuggestGrant || sg[0].Approved != 12 || sg[0].Approvers != 2 {
		t.Fatalf("suggestions = %+v", sg)
	}
	if !MatchArgs(sg[0].Match, restartStaging) || MatchArgs(sg[0].Match, raw(`{"workspace":"prod","app":"api"}`)) {
		t.Fatalf("suggested match is wider than the evidence: %+v", sg[0].Match)
	}
	g, err := s.AcceptSuggestion(ctx, org, "usr_1", ag.ID, sg[0].Key, 8, 0, "")
	if err != nil || g.AgentID == nil || *g.AgentID != ag.ID {
		t.Fatalf("accept: %v %+v", err, g)
	}
	if sg, _ := s.Suggestions(ctx, org, ag.ID); len(sg) != 0 {
		t.Fatalf("an accepted suggestion is still offered: %+v", sg)
	}
}

func TestSuggestionNeedsCleanEvidence(t *testing.T) {
	s, db, ag := newService(t)
	ctx := context.Background()
	start := s.now().Add(-20 * 24 * time.Hour)
	seedApprovals(db, ag, 12, models.ApprovalApproved, `{"workspace":"staging","app":"api"}`, start, "usr_1")
	if sg, _ := s.Suggestions(ctx, org, ag.ID); len(sg) != 0 {
		t.Fatal("suggested with a single approver")
	}
	seedApprovals(db, ag, 1, models.ApprovalApproved, `{"workspace":"staging","app":"api"}`, start, "usr_2")
	seedApprovals(db, ag, 1, models.ApprovalDenied, `{"workspace":"staging","app":"api"}`, start, "usr_2")
	if sg, _ := s.Suggestions(ctx, org, ag.ID); len(sg) != 0 {
		t.Fatal("suggested despite a decline")
	}
}

func TestDismissHoldsUntilNewEvidence(t *testing.T) {
	s, db, ag := newService(t)
	ctx := context.Background()
	start := s.now().Add(-20 * 24 * time.Hour)
	seedApprovals(db, ag, 12, models.ApprovalApproved, `{"workspace":"staging","app":"api"}`, start, "usr_1", "usr_2")
	sg, _ := s.Suggestions(ctx, org, ag.ID)
	if err := s.DismissSuggestion(ctx, org, "usr_1", ag.ID, sg[0].Key); err != nil {
		t.Fatal(err)
	}
	if sg, _ := s.Suggestions(ctx, org, ag.ID); len(sg) != 0 {
		t.Fatal("dismissed suggestion still shown")
	}
}
