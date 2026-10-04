// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package plans holds project plans: work on a project written by people as a description and
// ordered phases. Tasks are linked to plans and receive them in their goal; agents report progress
// on phases with plan_phase_update, and can't create, edit or delete plans.
package plans

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/audit"
	"github.com/goakili/akili/server/internal/bus"
	"github.com/goakili/akili/server/internal/models"
	"gorm.io/gorm"
)

// Limits keep a task's goal a reasonable size: every linked plan is in it.
const (
	MaxPhases       = 100
	MaxPerTask      = 10
	maxTitle        = 200
	maxPhaseTitle   = 300
	maxDescription  = 20000
	maxDetail       = 4000
	maxDoneWhen     = 2000
	EvPlanUpdated   = "plan.updated"
	agentUpdaterPfx = "agent:"
)

var (
	ErrNotFound = errors.New("plan not found")
	ErrInvalid  = errors.New("invalid plan")
)

// Emitter publishes live events (bus.Bus).
type Emitter interface {
	EmitData(ctx context.Context, org string, ev bus.Event, data any)
}

// Service stores plans and their phases.
type Service struct {
	db    *gorm.DB
	audit *audit.Logger
	bus   Emitter
}

// New returns the service.
func New(db *gorm.DB, a *audit.Logger, b Emitter) *Service { return &Service{db: db, audit: a, bus: b} }

// PhaseInput is a phase as a person writes it. ID keeps an existing phase (and its status).
type PhaseInput struct {
	ID       string `json:"id,omitempty"`
	Title    string `json:"title"`
	Detail   string `json:"detail,omitempty"`
	DoneWhen string `json:"done_when,omitempty" description:"what finished means for this phase"`
}

// CreateInput is a new plan.
type CreateInput struct {
	Title       string       `json:"title"`
	Description string       `json:"description"`
	Status      string       `json:"status" description:"draft or active (default)"`
	Phases      []PhaseInput `json:"phases"`
}

// UpdateInput changes a plan; nil fields are kept.
type UpdateInput struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Status      *string `json:"status" description:"draft, active or archived; in_progress and done follow the phases"`
	Position    *int    `json:"position"`
}

// Summary is a plan in a list, with its progress.
type Summary struct {
	models.ProjectPlan
	Phases int            `json:"phases"`
	Counts map[string]int `json:"counts"`
	Tasks  int            `json:"tasks"`
}

// Detail is a plan with its phases and the tasks linked to it.
type Detail struct {
	Plan   *models.ProjectPlan `json:"plan"`
	Phases []models.PlanPhase  `json:"phases"`
	Tasks  []models.Task       `json:"tasks"`
	// Links say which tasks work on the plan, and on which phase when a task is focused on one.
	Links []Link `json:"links"`
}

// Link is a task linked to a plan, focused on one phase or (PhaseID nil) on the whole plan.
type Link struct {
	TaskID  string  `json:"task_id"`
	PhaseID *string `json:"phase_id"`
}

func clean(s string) string { return strings.TrimSpace(s) }

func validTitle(s string, max int) bool {
	n := len([]rune(s))
	return n >= 1 && n <= max && !strings.ContainsAny(s, "\r\n")
}

func validatePhases(phases []PhaseInput) error {
	if len(phases) > MaxPhases {
		return fmt.Errorf("%w: at most %d phases", ErrInvalid, MaxPhases)
	}
	for i := range phases {
		phases[i].Title, phases[i].Detail, phases[i].DoneWhen = clean(phases[i].Title), clean(phases[i].Detail), clean(phases[i].DoneWhen)
		if !validTitle(phases[i].Title, maxPhaseTitle) {
			return fmt.Errorf("%w: phase %d needs a one-line title of 1-%d characters", ErrInvalid, i+1, maxPhaseTitle)
		}
		if len([]rune(phases[i].Detail)) > maxDetail {
			return fmt.Errorf("%w: phase %d detail is longer than %d characters", ErrInvalid, i+1, maxDetail)
		}
		if len([]rune(phases[i].DoneWhen)) > maxDoneWhen {
			return fmt.Errorf("%w: phase %d \"done when\" is longer than %d characters", ErrInvalid, i+1, maxDoneWhen)
		}
	}
	return nil
}

// Derive is the plan status its phases give it. Drafts and archived plans keep their status; a plan
// is done when every phase is done or skipped, and in progress once any phase has moved.
func Derive(current string, phases []models.PlanPhase) string {
	if current == models.PlanDraft || current == models.PlanArchived {
		return current
	}
	if len(phases) == 0 {
		return models.PlanActive
	}
	closed, moved := 0, 0
	for _, st := range phases {
		switch st.Status {
		case models.PhaseDone, models.PhaseSkipped:
			closed++
			moved++
		case models.PhaseInProgress:
			moved++
		}
	}
	switch {
	case closed == len(phases):
		return models.PlanDone
	case moved > 0:
		return models.PlanInProgress
	}
	return models.PlanActive
}

// MergePhases lines up a replacement phase list with the existing phases: known ids keep their row and
// status, unknown or empty ids become new todo phases, and phases left out are deleted.
func MergePhases(existing []models.PlanPhase, in []PhaseInput) (keep []models.PlanPhase, create []PhaseInput, drop []string, positions []int) {
	byID := map[string]models.PlanPhase{}
	for _, st := range existing {
		byID[st.ID] = st
	}
	used := map[string]bool{}
	for i, s := range in {
		if st, ok := byID[s.ID]; ok && !used[s.ID] {
			used[s.ID] = true
			st.Title, st.Detail, st.DoneWhen, st.Position = s.Title, s.Detail, s.DoneWhen, i
			keep = append(keep, st)
			continue
		}
		create = append(create, PhaseInput{Title: s.Title, Detail: s.Detail, DoneWhen: s.DoneWhen})
		positions = append(positions, i)
	}
	for _, st := range existing {
		if !used[st.ID] {
			drop = append(drop, st.ID)
		}
	}
	return keep, create, drop, positions
}

func (s *Service) emit(ctx context.Context, p *models.ProjectPlan) {
	if s.bus != nil {
		s.bus.EmitData(ctx, p.OrganizationID, bus.Event{Type: EvPlanUpdated}, map[string]string{"id": p.ID, "project_id": p.ProjectID, "status": p.Status})
	}
}

func (s *Service) plan(ctx context.Context, db *gorm.DB, org, id string) (*models.ProjectPlan, error) {
	var p models.ProjectPlan
	if err := db.WithContext(ctx).First(&p, "id = ? AND organization_id = ?", id, org).Error; err != nil {
		return nil, ErrNotFound
	}
	return &p, nil
}

func phases(ctx context.Context, db *gorm.DB, planID string) []models.PlanPhase {
	var out []models.PlanPhase
	db.WithContext(ctx).Where("plan_id = ?", planID).Order("position, created_at").Find(&out)
	return out
}

// recompute stores the status the phases give the plan.
func recompute(ctx context.Context, tx *gorm.DB, p *models.ProjectPlan) error {
	next := Derive(p.Status, phases(ctx, tx, p.ID))
	if next == p.Status {
		return nil
	}
	p.Status = next
	return tx.WithContext(ctx).Model(p).Update("status", next).Error
}

// List returns a project's plans with their progress, archived last.
func (s *Service) List(ctx context.Context, org, projectID string) ([]Summary, error) {
	var ps []models.ProjectPlan
	if err := s.db.WithContext(ctx).Where("organization_id = ? AND project_id = ?", org, projectID).
		Order("status = 'archived', position, created_at DESC").Find(&ps).Error; err != nil {
		return nil, err
	}
	out := make([]Summary, 0, len(ps))
	if len(ps) == 0 {
		return out, nil
	}
	ids := make([]string, len(ps))
	for i, p := range ps {
		ids[i] = p.ID
	}
	var counts []struct {
		PlanID, Status string
		N              int
	}
	s.db.WithContext(ctx).Model(&models.PlanPhase{}).Select("plan_id, status, count(*) AS n").Where("plan_id IN ?", ids).Group("plan_id, status").Scan(&counts)
	var links []struct {
		PlanID string
		N      int
	}
	s.db.WithContext(ctx).Model(&models.TaskPlan{}).Select("plan_id, count(*) AS n").Where("organization_id = ? AND plan_id IN ?", org, ids).Group("plan_id").Scan(&links)
	for _, p := range ps {
		sum := Summary{ProjectPlan: p, Counts: map[string]int{}}
		for _, c := range counts {
			if c.PlanID == p.ID {
				sum.Counts[c.Status] = c.N
				sum.Phases += c.N
			}
		}
		for _, l := range links {
			if l.PlanID == p.ID {
				sum.Tasks = l.N
			}
		}
		out = append(out, sum)
	}
	return out, nil
}

// Get returns a plan with its phases and linked tasks.
func (s *Service) Get(ctx context.Context, org, id string) (*Detail, error) {
	p, err := s.plan(ctx, s.db, org, id)
	if err != nil {
		return nil, err
	}
	d := &Detail{Plan: p, Phases: phases(ctx, s.db, p.ID), Tasks: []models.Task{}, Links: []Link{}}
	s.db.WithContext(ctx).Where("organization_id = ? AND id IN (?)", org,
		s.db.Model(&models.TaskPlan{}).Select("task_id").Where("plan_id = ?", p.ID)).Order("created_at DESC").Limit(100).Find(&d.Tasks)
	var links []models.TaskPlan
	s.db.WithContext(ctx).Select("task_id, phase_id").Where("organization_id = ? AND plan_id = ?", org, p.ID).Find(&links)
	for _, l := range links {
		d.Links = append(d.Links, Link{TaskID: l.TaskID, PhaseID: l.PhaseID})
	}
	return d, nil
}

// Create stores a plan with its phases.
func (s *Service) Create(ctx context.Context, org, userID, projectID string, in CreateInput) (*Detail, error) {
	in.Title, in.Description = clean(in.Title), clean(in.Description)
	if !validTitle(in.Title, maxTitle) {
		return nil, fmt.Errorf("%w: a plan needs a one-line title of 1-%d characters", ErrInvalid, maxTitle)
	}
	if len([]rune(in.Description)) > maxDescription {
		return nil, fmt.Errorf("%w: the description is longer than %d characters", ErrInvalid, maxDescription)
	}
	switch in.Status {
	case "":
		in.Status = models.PlanActive
	case models.PlanDraft, models.PlanActive:
	default:
		return nil, fmt.Errorf("%w: a new plan is draft or active", ErrInvalid)
	}
	if err := validatePhases(in.Phases); err != nil {
		return nil, err
	}
	var n int64
	s.db.WithContext(ctx).Model(&models.Project{}).Where("id = ? AND organization_id = ?", projectID, org).Count(&n)
	if n == 0 {
		return nil, ErrNotFound
	}
	p := &models.ProjectPlan{Base: models.Base{ID: models.NewID("pln"), OrganizationID: org}, ProjectID: projectID, Title: in.Title,
		Description: in.Description, Status: in.Status, CreatedBy: userID}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(p).Error; err != nil {
			return err
		}
		for i, st := range in.Phases {
			if err := tx.Create(&models.PlanPhase{Base: models.Base{ID: models.NewID("phs"), OrganizationID: org}, PlanID: p.ID, Position: i,
				Title: st.Title, Detail: st.Detail, DoneWhen: st.DoneWhen, Status: models.PhaseTodo, UpdatedBy: userID}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.emit(ctx, p)
	return s.Get(ctx, org, p.ID)
}

// Update changes a plan's text, order or status.
func (s *Service) Update(ctx context.Context, org, id string, in UpdateInput) (*models.ProjectPlan, error) {
	p, err := s.plan(ctx, s.db, org, id)
	if err != nil {
		return nil, err
	}
	set := map[string]any{}
	if in.Title != nil {
		t := clean(*in.Title)
		if !validTitle(t, maxTitle) {
			return nil, fmt.Errorf("%w: a plan needs a one-line title of 1-%d characters", ErrInvalid, maxTitle)
		}
		set["title"], p.Title = t, t
	}
	if in.Description != nil {
		d := clean(*in.Description)
		if len([]rune(d)) > maxDescription {
			return nil, fmt.Errorf("%w: the description is longer than %d characters", ErrInvalid, maxDescription)
		}
		set["description"], p.Description = d, d
	}
	if in.Position != nil {
		set["position"], p.Position = *in.Position, *in.Position
	}
	if in.Status != nil {
		switch *in.Status {
		case models.PlanDraft, models.PlanArchived:
			set["status"], p.Status = *in.Status, *in.Status
		case models.PlanActive:
			// Activating, or reopening a done plan: the phases decide from here.
			p.Status = Derive(models.PlanActive, phases(ctx, s.db, p.ID))
			set["status"] = p.Status
		default:
			return nil, fmt.Errorf("%w: status is draft, active or archived", ErrInvalid)
		}
	}
	if len(set) > 0 {
		if err := s.db.WithContext(ctx).Model(p).Updates(set).Error; err != nil {
			return nil, err
		}
		s.emit(ctx, p)
	}
	return p, nil
}

// ErrInUse refuses to delete a plan an unfinished task works on.
var ErrInUse = errors.New("an unfinished task is linked to this plan; cancel it or wait for it to finish")

// Delete removes a plan, its phases and its task links.
func (s *Service) Delete(ctx context.Context, org, id string) error {
	p, err := s.plan(ctx, s.db, org, id)
	if err != nil {
		return err
	}
	var open int64
	s.db.WithContext(ctx).Model(&models.Task{}).Where("organization_id = ? AND status NOT IN ? AND id IN (?)", org,
		[]string{models.TaskSucceeded, models.TaskFailed, models.TaskCancelled, models.TaskTimedOut},
		s.db.Model(&models.TaskPlan{}).Select("task_id").Where("plan_id = ?", p.ID)).Count(&open)
	if open > 0 {
		return ErrInUse
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("plan_id = ?", p.ID).Delete(&models.PlanPhase{}).Error; err != nil {
			return err
		}
		if err := tx.Where("plan_id = ?", p.ID).Delete(&models.TaskPlan{}).Error; err != nil {
			return err
		}
		return tx.Delete(p).Error
	})
	if err == nil {
		s.emit(ctx, p)
	}
	return err
}

// ReplacePhases sets a plan's phase list: kept phases keep their id and status, new ones start todo.
func (s *Service) ReplacePhases(ctx context.Context, org, userID, id string, in []PhaseInput) (*Detail, error) {
	if err := validatePhases(in); err != nil {
		return nil, err
	}
	p, err := s.plan(ctx, s.db, org, id)
	if err != nil {
		return nil, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		keep, create, drop, positions := MergePhases(phases(ctx, tx, p.ID), in)
		if len(drop) > 0 {
			if err := tx.Where("plan_id = ? AND id IN ?", p.ID, drop).Delete(&models.PlanPhase{}).Error; err != nil {
				return err
			}
		}
		for _, st := range keep {
			if err := tx.Model(&models.PlanPhase{}).Where("id = ?", st.ID).
				Updates(map[string]any{"title": st.Title, "detail": st.Detail, "done_when": st.DoneWhen, "position": st.Position}).Error; err != nil {
				return err
			}
		}
		for i, st := range create {
			if err := tx.Create(&models.PlanPhase{Base: models.Base{ID: models.NewID("phs"), OrganizationID: org}, PlanID: p.ID,
				Position: positions[i], Title: st.Title, Detail: st.Detail, DoneWhen: st.DoneWhen, Status: models.PhaseTodo, UpdatedBy: userID}).Error; err != nil {
				return err
			}
		}
		return recompute(ctx, tx, p)
	})
	if err != nil {
		return nil, err
	}
	s.emit(ctx, p)
	return s.Get(ctx, org, p.ID)
}

func validPhaseStatus(st string) bool {
	return st == models.PhaseTodo || st == models.PhaseInProgress || st == models.PhaseDone || st == models.PhaseSkipped
}

// SetPhase changes one phase's status and note, as a person.
func (s *Service) SetPhase(ctx context.Context, org, userID, planID, phaseID, status, note string) (*models.PlanPhase, error) {
	if !validPhaseStatus(status) {
		return nil, fmt.Errorf("%w: status is todo, in_progress, done or skipped", ErrInvalid)
	}
	if len([]rune(note)) > proto.MaxPlanNote {
		return nil, fmt.Errorf("%w: a note is at most %d characters", ErrInvalid, proto.MaxPlanNote)
	}
	return s.setPhase(ctx, org, planID, phaseID, status, clean(note), nil, userID)
}

func (s *Service) setPhase(ctx context.Context, org, planID, phaseID, status, note string, task *string, by string) (*models.PlanPhase, error) {
	p, err := s.plan(ctx, s.db, org, planID)
	if err != nil {
		return nil, err
	}
	var st models.PlanPhase
	if err := s.db.WithContext(ctx).First(&st, "id = ? AND plan_id = ?", phaseID, p.ID).Error; err != nil {
		return nil, fmt.Errorf("%w: no such phase in this plan", ErrInvalid)
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		st.Status, st.Note, st.DoneByTask, st.UpdatedBy = status, note, task, by
		if err := tx.Model(&st).Updates(map[string]any{"status": status, "note": note, "done_by_task": task, "updated_by": by}).Error; err != nil {
			return err
		}
		return recompute(ctx, tx, p)
	})
	if err != nil {
		return nil, err
	}
	s.emit(ctx, p)
	return &st, nil
}

// Attach links plans to a new task inside its creation transaction, with a snapshot of each. The
// plans must belong to the task's project and be active or in progress. phaseID, when set, focuses
// the task on that phase (its plan is linked too); the phase must still be open.
func Attach(ctx context.Context, tx *gorm.DB, org, projectID, taskID string, ids []string, phaseID string) error {
	var focus *models.PlanPhase
	if phaseID != "" {
		var ph models.PlanPhase
		if err := tx.WithContext(ctx).First(&ph, "id = ? AND organization_id = ?", phaseID, org).Error; err != nil {
			return fmt.Errorf("%w: unknown phase", ErrInvalid)
		}
		if ph.Status == models.PhaseDone || ph.Status == models.PhaseSkipped {
			return fmt.Errorf("%w: phase %q is %s", ErrInvalid, ph.Title, ph.Status)
		}
		focus = &ph
		ids = append(ids, ph.PlanID)
	}
	seen := map[string]bool{}
	var uniq []string
	for _, id := range ids {
		if id != "" && !seen[id] {
			seen[id] = true
			uniq = append(uniq, id)
		}
	}
	if len(uniq) == 0 {
		return nil
	}
	if projectID == "" {
		return fmt.Errorf("%w: plans can only be linked to a project task", ErrInvalid)
	}
	if len(uniq) > MaxPerTask {
		return fmt.Errorf("%w: a task links at most %d plans", ErrInvalid, MaxPerTask)
	}
	var ps []models.ProjectPlan
	tx.WithContext(ctx).Where("organization_id = ? AND project_id = ? AND id IN ?", org, projectID, uniq).Find(&ps)
	if len(ps) != len(uniq) {
		return fmt.Errorf("%w: every plan must belong to the task's project", ErrInvalid)
	}
	for _, p := range ps {
		if p.Status != models.PlanActive && p.Status != models.PlanInProgress {
			return fmt.Errorf("%w: plan %q is %s; only active plans can be linked", ErrInvalid, p.Title, p.Status)
		}
		snap := models.PlanSnapshot{Title: p.Title, Description: p.Description, Phases: []models.PlanSnapshotPhase{}}
		for _, st := range phases(ctx, tx, p.ID) {
			snap.Phases = append(snap.Phases, models.PlanSnapshotPhase{ID: st.ID, Title: st.Title, Detail: st.Detail, DoneWhen: st.DoneWhen, Status: st.Status})
		}
		link := &models.TaskPlan{OrganizationID: org, TaskID: taskID, PlanID: p.ID, Snapshot: snap}
		if focus != nil && focus.PlanID == p.ID {
			link.PhaseID = &focus.ID
		}
		if err := tx.Create(link).Error; err != nil {
			return err
		}
	}
	return nil
}

// ForTask returns the plans linked to a task, with their snapshots.
func (s *Service) ForTask(ctx context.Context, org, taskID string) []models.TaskPlan {
	var out []models.TaskPlan
	s.db.WithContext(ctx).Where("organization_id = ? AND task_id = ?", org, taskID).Order("created_at, plan_id").Find(&out)
	return out
}

// PlanIDs returns the linked plan ids of each task, for annotating task lists.
func (s *Service) PlanIDs(ctx context.Context, org string, taskIDs []string) map[string][]string {
	out := map[string][]string{}
	if len(taskIDs) == 0 {
		return out
	}
	var rows []models.TaskPlan
	s.db.WithContext(ctx).Select("task_id, plan_id").Where("organization_id = ? AND task_id IN ?", org, taskIDs).Order("created_at, plan_id").Find(&rows)
	for _, r := range rows {
		out[r.TaskID] = append(out[r.TaskID], r.PlanID)
	}
	return out
}

// GoalSection renders the linked plans for the task's goal (the first user turn, not the system
// prompt), from the snapshots taken when the task was created.
func GoalSection(links []models.TaskPlan) string {
	if len(links) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n## Plans\n\nThis task works on the project plans below. Work on the phases that are not done or skipped. " +
		"Report progress with plan_phase_update: in_progress when you start a phase, done when it is finished, " +
		"skipped (with a note saying why) when it is not needed. Use the plan and phase ids shown in brackets.\n")
	indent := func(s string) string { return strings.ReplaceAll(s, "\n", "\n  ") }
	for _, l := range links {
		fmt.Fprintf(&b, "\n### %s [plan %s]\n", l.Snapshot.Title, l.PlanID)
		if l.PhaseID != nil {
			for _, st := range l.Snapshot.Phases {
				if st.ID == *l.PhaseID {
					fmt.Fprintf(&b, "\nThis task works on one phase of this plan: [%s] %s. Finish that phase only; the other phases are context, "+
						"and you can report progress on that phase only.\n", st.ID, st.Title)
				}
			}
		}
		if l.Snapshot.Description != "" {
			b.WriteString("\n" + l.Snapshot.Description + "\n")
		}
		if len(l.Snapshot.Phases) > 0 {
			b.WriteString("\nPhases:\n")
		}
		for _, st := range l.Snapshot.Phases {
			mark := ""
			if l.PhaseID != nil && st.ID == *l.PhaseID {
				mark = " ← this task"
			}
			fmt.Fprintf(&b, "- [%s] (%s) %s%s\n", st.ID, st.Status, st.Title, mark)
			if st.Detail != "" {
				b.WriteString("  " + indent(st.Detail) + "\n")
			}
			if st.DoneWhen != "" {
				b.WriteString("  Done when: " + indent(st.DoneWhen) + "\n")
			}
		}
	}
	return b.String()
}

// RunRemote executes plan_phase_update for a task session: only on a plan linked to that task.
func (s *Service) RunRemote(ctx context.Context, sess *models.ChatSession, _ string, input json.RawMessage) proto.RemoteResult {
	fail := func(msg string) proto.RemoteResult { return proto.RemoteResult{Output: "error: " + msg, IsError: true} }
	var in proto.PlanPhaseInput
	if err := json.Unmarshal(input, &in); err != nil {
		return fail("invalid input")
	}
	if sess.TaskID == nil {
		return fail("plan_phase_update works only in a task that a plan is linked to")
	}
	var link models.TaskPlan
	if err := s.db.WithContext(ctx).Select("task_id, plan_id, phase_id").
		First(&link, "organization_id = ? AND task_id = ? AND plan_id = ?", sess.OrganizationID, *sess.TaskID, in.Plan).Error; err != nil {
		return fail("plan " + in.Plan + " is not linked to this task")
	}
	if link.PhaseID != nil && in.Phase != *link.PhaseID {
		return fail("this task works only on phase " + *link.PhaseID + " of plan " + in.Plan)
	}
	p, err := s.plan(ctx, s.db, sess.OrganizationID, in.Plan)
	if err != nil {
		return fail("plan " + in.Plan + " no longer exists")
	}
	if p.Status == models.PlanDraft || p.Status == models.PlanArchived {
		return fail("plan " + in.Plan + " is " + p.Status + "; its phases can't be changed")
	}
	if in.Status != models.PhaseInProgress && in.Status != models.PhaseDone && in.Status != models.PhaseSkipped {
		return fail("status must be in_progress, done or skipped")
	}
	note := clean(in.Note)
	if len([]rune(note)) > proto.MaxPlanNote {
		return fail(fmt.Sprintf("a note is at most %d characters", proto.MaxPlanNote))
	}
	st, err := s.setPhase(ctx, sess.OrganizationID, p.ID, in.Phase, in.Status, note, sess.TaskID, agentUpdaterPfx+sess.AgentID)
	if err != nil {
		return fail(strings.TrimPrefix(err.Error(), ErrInvalid.Error()+": "))
	}
	s.audit.Best(ctx, audit.Entry{OrganizationID: sess.OrganizationID, ActorType: audit.ActorAgent, ActorID: sess.AgentID, Action: "plan.phase_update",
		TargetType: "plan", TargetID: p.ID, Metadata: map[string]any{"phase": st.ID, "status": st.Status, "task_id": *sess.TaskID, "session_id": sess.ID}})
	all := phases(ctx, s.db, p.ID)
	closed := 0
	for _, x := range all {
		if x.Status == models.PhaseDone || x.Status == models.PhaseSkipped {
			closed++
		}
	}
	return proto.RemoteResult{Output: fmt.Sprintf("Phase %q is now %s. Plan %q: %d of %d phases done or skipped.", st.Title, st.Status, p.Title, closed, len(all))}
}

// IsAgentUpdate reports whether a phase's last change came from an agent.
func IsAgentUpdate(st models.PlanPhase) bool { return strings.HasPrefix(st.UpdatedBy, agentUpdaterPfx) }
