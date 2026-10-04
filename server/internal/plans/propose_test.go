// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package plans

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/audit"
	"github.com/goakili/akili/server/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func proposeService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Project{}, &models.ProjectPlan{}, &models.PlanPhase{}, &models.TaskPlan{}, &models.Task{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Project{Base: models.Base{ID: "prj_1", OrganizationID: "org_1"}, Name: "p", Slug: "p", IntegrationID: "int_1",
		Owner: "o", Repo: "r", DefaultBranch: "main", Selector: []string{}}).Error; err != nil {
		t.Fatal(err)
	}
	return New(db, audit.New(db), nil), db
}

func proposal(title string, phases ...string) json.RawMessage {
	in := proto.PlanProposeInput{Title: title, Description: "Why and how."}
	for _, p := range phases {
		in.Phases = append(in.Phases, proto.PlanProposalPhase{Title: p, DoneWhen: p + " is merged"})
	}
	b, _ := json.Marshal(in)
	return b
}

func TestProposeCreatesADraft(t *testing.T) {
	s, db := proposeService(t)
	ctx := context.Background()
	prj, task := "prj_1", "tsk_1"
	sess := &models.ChatSession{Base: models.Base{ID: "ses_1", OrganizationID: "org_1"}, AgentID: "ag_1", ProjectID: &prj, TaskID: &task}

	res := s.RunRemote(ctx, sess, proto.ToolPlanPropose, proposal("Version endpoint", "Add /version", "Document it"))
	if res.IsError || !strings.Contains(res.Output, "draft") {
		t.Fatalf("propose: %+v", res)
	}
	var p models.ProjectPlan
	if err := db.First(&p, "project_id = ?", prj).Error; err != nil {
		t.Fatal(err)
	}
	if p.Status != models.PlanDraft || p.CreatedBy != "agent:ag_1" || p.ProposedSessionID == nil || *p.ProposedSessionID != "ses_1" {
		t.Fatalf("plan = %+v", p)
	}
	ph := phases(ctx, db, p.ID)
	if len(ph) != 2 || ph[0].Title != "Add /version" || ph[0].Status != models.PhaseTodo || ph[0].UpdatedBy != "" || ph[1].DoneWhen != "Document it is merged" {
		t.Fatalf("phases = %+v", ph)
	}
	// The audit entry needs Postgres (advisory lock), so e2e-coder checks it.
	// Nothing works on a proposal until a person activates it.
	if err := Attach(ctx, db, "org_1", prj, "tsk_2", []string{p.ID}, ""); err == nil {
		t.Fatal("a proposed draft was linked to a task")
	}
	if _, err := s.Update(ctx, "org_1", p.ID, UpdateInput{Status: ptr(models.PlanActive)}); err != nil {
		t.Fatal(err)
	}
	if err := Attach(ctx, db, "org_1", prj, "tsk_2", []string{p.ID}, ""); err != nil {
		t.Fatalf("activated proposal can't be linked: %v", err)
	}
}

func TestProposeLimits(t *testing.T) {
	s, _ := proposeService(t)
	ctx := context.Background()
	prj := "prj_1"
	sess := &models.ChatSession{Base: models.Base{ID: "ses_1", OrganizationID: "org_1"}, AgentID: "ag_1", ProjectID: &prj}

	if res := s.RunRemote(ctx, &models.ChatSession{Base: models.Base{ID: "ses_x", OrganizationID: "org_1"}, AgentID: "ag_1"}, proto.ToolPlanPropose, proposal("No project", "a")); !res.IsError {
		t.Fatal("proposed outside a project session")
	}
	s.RunRemote(ctx, sess, proto.ToolPlanPropose, proposal("Same plan", "a"))
	if res := s.RunRemote(ctx, sess, proto.ToolPlanPropose, proposal("same PLAN", "b")); res.IsError || !strings.Contains(res.Output, "already waiting") {
		t.Fatalf("duplicate: %+v", res)
	}
	for i := 1; i < MaxProposalsPerSession; i++ {
		if res := s.RunRemote(ctx, sess, proto.ToolPlanPropose, proposal("Plan "+string(rune('A'+i)), "a")); res.IsError {
			t.Fatalf("proposal %d: %s", i, res.Output)
		}
	}
	if res := s.RunRemote(ctx, sess, proto.ToolPlanPropose, proposal("One too many", "a")); !res.IsError {
		t.Fatal("the per-session limit was not applied")
	}
}

func ptr[T any](v T) *T { return &v }
