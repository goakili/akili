// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package plans

import (
	"errors"
	"strings"
	"testing"

	"github.com/goakili/akili/server/internal/models"
)

func phase(id, status string) models.PlanPhase {
	return models.PlanPhase{Base: models.Base{ID: id}, Status: status, Title: id}
}

func TestDerive(t *testing.T) {
	tests := []struct {
		name    string
		current string
		phases  []models.PlanPhase
		want    string
	}{
		{"draft stays draft", models.PlanDraft, []models.PlanPhase{phase("a", models.PhaseDone)}, models.PlanDraft},
		{"archived stays archived", models.PlanArchived, nil, models.PlanArchived},
		{"no phases is active", models.PlanInProgress, nil, models.PlanActive},
		{"all todo is active", models.PlanActive, []models.PlanPhase{phase("a", models.PhaseTodo), phase("b", models.PhaseTodo)}, models.PlanActive},
		{"one started", models.PlanActive, []models.PlanPhase{phase("a", models.PhaseInProgress), phase("b", models.PhaseTodo)}, models.PlanInProgress},
		{"one done", models.PlanActive, []models.PlanPhase{phase("a", models.PhaseDone), phase("b", models.PhaseTodo)}, models.PlanInProgress},
		{"done and skipped is done", models.PlanInProgress, []models.PlanPhase{phase("a", models.PhaseDone), phase("b", models.PhaseSkipped)}, models.PlanDone},
		{"reopened phase reopens the plan", models.PlanDone, []models.PlanPhase{phase("a", models.PhaseDone), phase("b", models.PhaseTodo)}, models.PlanInProgress},
	}
	for _, tt := range tests {
		if got := Derive(tt.current, tt.phases); got != tt.want {
			t.Errorf("%s: got %s, want %s", tt.name, got, tt.want)
		}
	}
}

func TestMergePhases(t *testing.T) {
	existing := []models.PlanPhase{phase("a", models.PhaseDone), phase("b", models.PhaseInProgress), phase("c", models.PhaseTodo)}
	keep, create, drop, pos := MergePhases(existing, []PhaseInput{
		{ID: "b", Title: "B renamed"},
		{Title: "new one"},
		{ID: "a", Title: "A"},
		{ID: "a", Title: "duplicate id becomes new"},
		{ID: "zzz", Title: "unknown id becomes new"},
	})
	if len(keep) != 2 || keep[0].ID != "b" || keep[0].Title != "B renamed" || keep[0].Position != 0 || keep[0].Status != models.PhaseInProgress ||
		keep[1].ID != "a" || keep[1].Position != 2 || keep[1].Status != models.PhaseDone {
		t.Fatalf("keep: %+v", keep)
	}
	if len(create) != 3 || create[0].Title != "new one" || create[1].ID != "" || create[2].ID != "" {
		t.Fatalf("create: %+v", create)
	}
	if len(pos) != 3 || pos[0] != 1 || pos[1] != 3 || pos[2] != 4 {
		t.Fatalf("positions: %v", pos)
	}
	if len(drop) != 1 || drop[0] != "c" {
		t.Fatalf("drop: %v", drop)
	}
}

func TestValidatePhases(t *testing.T) {
	ok := []PhaseInput{{Title: "  Add the endpoint  ", Detail: " with a test "}}
	if err := validatePhases(ok); err != nil || ok[0].Title != "Add the endpoint" || ok[0].Detail != "with a test" {
		t.Fatalf("valid phases: %v %+v", err, ok)
	}
	for name, bad := range map[string][]PhaseInput{
		"empty title":     {{Title: "  "}},
		"two-line title":  {{Title: "a\nb"}},
		"long title":      {{Title: strings.Repeat("x", maxPhaseTitle+1)}},
		"long detail":     {{Title: "x", Detail: strings.Repeat("x", maxDetail+1)}},
		"too many phases": make([]PhaseInput, MaxPhases+1),
	} {
		if err := validatePhases(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

func TestGoalSection(t *testing.T) {
	if GoalSection(nil) != "" {
		t.Fatal("no plans should add nothing")
	}
	got := GoalSection([]models.TaskPlan{{PlanID: "pln_1", Snapshot: models.PlanSnapshot{
		Title: "Version endpoint", Description: "Expose the build version.",
		Phases: []models.PlanSnapshotPhase{{ID: "phs_1", Title: "Add /version", Status: models.PhaseTodo, Detail: "JSON\nwith commit"}, {ID: "phs_2", Title: "Document it", Status: models.PhaseDone}},
	}}})
	for _, want := range []string{"## Plans", "plan_phase_update", "### Version endpoint [plan pln_1]", "Expose the build version.",
		"- [phs_1] (todo) Add /version", "  JSON\n  with commit", "- [phs_2] (done) Document it"} {
		if !strings.Contains(got, want) {
			t.Errorf("goal section lacks %q:\n%s", want, got)
		}
	}
}

func TestGoalSectionFocus(t *testing.T) {
	focus := "phs_2"
	got := GoalSection([]models.TaskPlan{{PlanID: "pln_1", PhaseID: &focus, Snapshot: models.PlanSnapshot{
		Title: "Version endpoint",
		Phases: []models.PlanSnapshotPhase{
			{ID: "phs_1", Title: "Add /version", Status: models.PhaseDone},
			{ID: "phs_2", Title: "Document it", Status: models.PhaseTodo, DoneWhen: "the README shows\nan example"},
		},
	}}})
	for _, want := range []string{
		"This task works on one phase of this plan: [phs_2] Document it. Finish that phase only",
		"- [phs_2] (todo) Document it ← this task",
		"  Done when: the README shows\n  an example",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("goal section lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Add /version ← this task") {
		t.Errorf("an unfocused phase is marked as the task's:\n%s", got)
	}
}

func TestMergePhasesKeepsDoneWhen(t *testing.T) {
	keep, create, _, _ := MergePhases([]models.PlanPhase{phase("a", models.PhaseDone)},
		[]PhaseInput{{ID: "a", Title: "A", DoneWhen: "merged"}, {Title: "B", DoneWhen: "released"}})
	if keep[0].DoneWhen != "merged" || create[0].DoneWhen != "released" {
		t.Fatalf("done when not carried: %+v %+v", keep, create)
	}
	long := []PhaseInput{{Title: "x", DoneWhen: strings.Repeat("x", maxDoneWhen+1)}}
	if err := validatePhases(long); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a long done-when was accepted: %v", err)
	}
}
