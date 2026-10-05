// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package tasks queues work for agents, dispatches it with leases, retries lost work and runs
// schedules. The tasks table is the source of truth; Redis only wakes the dispatcher.
package tasks

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/audit"
	"github.com/goakili/akili/server/internal/bus"
	"github.com/goakili/akili/server/internal/leader"
	"github.com/goakili/akili/server/internal/models"
	"github.com/goakili/akili/server/internal/notify"
	"github.com/goakili/akili/server/internal/plans"
	"github.com/goakili/akili/server/internal/sessions"
	"github.com/jkaninda/logger"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Lease is how long an assigned task may go without a progress frame before it is considered lost.
const Lease = 3 * time.Minute

// Turn limits applied when a task does not set one. Coding tasks edit many files, so they get more.
const (
	DefaultMaxTurns        = 40
	DefaultProjectMaxTurns = 80
)

// Event types.
const (
	EvTaskUpdated = "task.updated"
)

// Service is the task service.
type Service struct {
	db     *gorm.DB
	bus    *bus.Bus
	audit  *audit.Logger
	hub    *sessions.Hub
	notify *notify.Notifier
	elect  *leader.Elector
}

// New returns the task service and registers it with the session hub.
func New(db *gorm.DB, b *bus.Bus, a *audit.Logger, hub *sessions.Hub, n *notify.Notifier, e *leader.Elector) *Service {
	s := &Service{db: db, bus: b, audit: a, hub: hub, notify: n, elect: e}
	hub.SetTaskHooks(s)
	return s
}

// Input creates a task.
type Input struct {
	Title       string
	Goal        string
	AgentID     *string
	Selector    []string
	Priority    int
	Autonomy    proto.Autonomy
	BudgetUSD   float64
	MaxTurns    int
	TimeoutSec  int
	MaxAttempts int
	ScheduleID  *string
	ProjectID   *string
	Trigger     string
	TriggerRef  string
	// PlanIDs links project plans; the agent receives them in its goal.
	PlanIDs []string
	// PlanPhaseID focuses the task on one phase of a plan (that plan is linked too).
	PlanPhaseID string
	// Draft saves the task without queueing it; Start queues it later.
	Draft bool
}

// DefaultAutonomy is used when a task does not set one. Project tasks run at L2: file edits, commits,
// pushes and pull requests proceed unattended (the git proxy still refuses anything but the task's
// own branch), while host shell commands still need approval.
func DefaultAutonomy(projectID *string) proto.Autonomy {
	if projectID != nil && *projectID != "" {
		return proto.AutonomyL2
	}
	return proto.AutonomyL1
}

// ErrNotFound is returned for unknown tasks.
var ErrNotFound = errors.New("task not found")

// Create queues a task.
func (s *Service) Create(ctx context.Context, org, userID string, in Input) (*models.Task, error) {
	if strings.TrimSpace(in.Goal) == "" {
		return nil, errors.New("goal is required")
	}
	if !in.Autonomy.Valid() {
		return nil, errors.New("autonomy must be 0-3")
	}
	if in.Title == "" {
		in.Title = firstLine(in.Goal, 80)
	}
	if in.AgentID != nil && *in.AgentID == "" {
		in.AgentID = nil
	}
	if in.ProjectID != nil && *in.ProjectID == "" {
		in.ProjectID = nil
	}
	if in.ProjectID != nil {
		var p models.Project
		if err := s.db.WithContext(ctx).First(&p, "id = ? AND organization_id = ?", *in.ProjectID, org).Error; err != nil {
			return nil, errors.New("unknown project")
		}
		// Without an explicit target, a project's tasks go to its preferred agent or labels.
		if in.AgentID == nil && len(in.Selector) == 0 {
			in.AgentID, in.Selector = p.AgentID, p.Selector
		}
	}
	if in.Trigger == "" {
		in.Trigger = "manual"
		if in.ScheduleID != nil {
			in.Trigger = "schedule"
		}
	}
	if in.AgentID != nil {
		var n int64
		s.db.WithContext(ctx).Model(&models.Agent{}).Where("id = ? AND organization_id = ?", *in.AgentID, org).Count(&n)
		if n == 0 {
			return nil, errors.New("unknown agent")
		}
	}
	if in.MaxAttempts <= 0 {
		in.MaxAttempts = 2
	}
	if in.TimeoutSec <= 0 {
		in.TimeoutSec = 3600
	}
	if in.MaxTurns <= 0 {
		in.MaxTurns = DefaultMaxTurns
		if in.ProjectID != nil {
			in.MaxTurns = DefaultProjectMaxTurns
		}
	}
	if in.Selector == nil {
		in.Selector = []string{}
	}
	status := models.TaskQueued
	if in.Draft {
		status = models.TaskDraft
	}
	t := &models.Task{Base: models.Base{ID: models.NewID("tsk"), OrganizationID: org}, Title: in.Title, Goal: in.Goal,
		AgentID: in.AgentID, Selector: in.Selector, Status: status, Priority: in.Priority, Autonomy: in.Autonomy,
		BudgetUSD: in.BudgetUSD, MaxTurns: in.MaxTurns, TimeoutSec: in.TimeoutSec, MaxAttempts: in.MaxAttempts,
		ScheduleID: in.ScheduleID, CreatedBy: userID, ProjectID: in.ProjectID, Trigger: in.Trigger, TriggerRef: in.TriggerRef}
	if t.ProjectID != nil {
		t.Branch = "akili/" + t.ID
	}
	projectID := ""
	if t.ProjectID != nil {
		projectID = *t.ProjectID
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(t).Error; err != nil {
			return err
		}
		return plans.Attach(ctx, tx, org, projectID, t.ID, in.PlanIDs, in.PlanPhaseID)
	})
	if err != nil {
		if errors.Is(err, plans.ErrInvalid) {
			return nil, ErrInvalidPlans{err}
		}
		return nil, err
	}
	t.PlanIDs = in.PlanIDs
	actorType := audit.ActorUser
	if userID == "" {
		actorType = audit.ActorSystem
	}
	s.audit.Best(ctx, audit.Entry{OrganizationID: org, ActorType: actorType, ActorID: userID, Action: "task.create", TargetType: "task", TargetID: t.ID,
		Metadata: map[string]any{"title": t.Title, "agent_id": t.AgentID, "selector": t.Selector, "autonomy": int(t.Autonomy), "budget_usd": t.BudgetUSD,
			"project_id": t.ProjectID, "trigger": t.Trigger, "plan_ids": in.PlanIDs, "plan_phase_id": in.PlanPhaseID, "draft": in.Draft}})
	s.emit(ctx, t)
	if !in.Draft {
		s.bus.Wake(ctx)
	}
	return t, nil
}

// ErrInvalidPlans wraps a refused plan link, so handlers can answer 400.
type ErrInvalidPlans struct{ Err error }

func (e ErrInvalidPlans) Error() string { return e.Err.Error() }
func (e ErrInvalidPlans) Unwrap() error { return e.Err }

// ErrNotDraft is returned when Start is called on a task that is not a draft.
var ErrNotDraft = errors.New("only a draft task can be started")

// Start queues a draft task.
func (s *Service) Start(ctx context.Context, org, userID, id string) (*models.Task, error) {
	res := s.db.WithContext(ctx).Model(&models.Task{}).Where("id = ? AND organization_id = ? AND status = ?", id, org, models.TaskDraft).
		Update("status", models.TaskQueued)
	if res.Error != nil {
		return nil, res.Error
	}
	t, err := s.Get(ctx, org, id)
	if err != nil {
		return nil, err
	}
	if res.RowsAffected == 0 {
		return nil, ErrNotDraft
	}
	s.audit.Best(ctx, audit.Entry{OrganizationID: org, ActorType: audit.ActorUser, ActorID: userID, Action: "task.run", TargetType: "task", TargetID: t.ID})
	s.emit(ctx, t)
	s.bus.Wake(ctx)
	return t, nil
}

// Get loads a task.
func (s *Service) Get(ctx context.Context, org, id string) (*models.Task, error) {
	var t models.Task
	if err := s.db.WithContext(ctx).First(&t, "id = ? AND organization_id = ?", id, org).Error; err != nil {
		return nil, ErrNotFound
	}
	return &t, nil
}

// List returns tasks, newest first.
func (s *Service) List(ctx context.Context, org, status string, limit int, projectID ...string) ([]models.Task, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	q := s.db.WithContext(ctx).Where("organization_id = ?", org)
	if status != "" {
		q = q.Where("status IN ?", strings.Split(status, ","))
	}
	if len(projectID) > 0 && projectID[0] != "" {
		q = q.Where("project_id = ?", projectID[0])
	}
	var out []models.Task
	return out, q.Order("created_at DESC").Limit(limit).Find(&out).Error
}

// Cancel stops a task. A queued task is cancelled at once; a running one is interrupted.
func (s *Service) Cancel(ctx context.Context, org, userID, id string) (*models.Task, error) {
	t, err := s.Get(ctx, org, id)
	if err != nil {
		return nil, err
	}
	if models.TaskTerminal(t.Status) {
		return t, nil
	}
	if t.SessionID != nil && t.AssignedAgentID != nil {
		_ = s.bus.SendCommand(ctx, *t.AssignedAgentID, bus.Command{Type: bus.CmdInterrupt, SessionID: *t.SessionID})
		_ = s.hub.Close(ctx, org, *t.SessionID, userID)
		s.hub.SessionLost(ctx, *t.SessionID, "task cancelled") // expires its pending approvals
	}
	s.finish(ctx, t, models.TaskCancelled, "", "cancelled by operator")
	s.audit.Best(ctx, audit.Entry{OrganizationID: org, ActorType: audit.ActorUser, ActorID: userID, Action: "task.cancel", TargetType: "task", TargetID: t.ID})
	return s.Get(ctx, org, id)
}

// Retry requeues a finished task as a new attempt series.
func (s *Service) Retry(ctx context.Context, org, userID, id string) (*models.Task, error) {
	t, err := s.Get(ctx, org, id)
	if err != nil {
		return nil, err
	}
	if !models.TaskTerminal(t.Status) {
		return nil, errors.New("only finished tasks can be retried")
	}
	return s.Create(ctx, org, userID, Input{Title: t.Title, Goal: t.Goal, AgentID: t.AgentID, Selector: t.Selector, Priority: t.Priority,
		Autonomy: t.Autonomy, BudgetUSD: t.BudgetUSD, MaxTurns: t.MaxTurns, TimeoutSec: t.TimeoutSec, MaxAttempts: t.MaxAttempts, ProjectID: t.ProjectID})
}

// ErrNotContinuable is returned when a task has no stopped run to continue.
var ErrNotContinuable = errors.New("only failed, timed-out or cancelled tasks that ran can be continued")

// Continue requeues a stopped task on the agent that ran it. The next run starts from the previous
// run's conversation, so the agent picks up where it stopped. maxTurns > 0 replaces the turn limit.
func (s *Service) Continue(ctx context.Context, org, userID, id string, maxTurns int) (*models.Task, error) {
	t, err := s.Get(ctx, org, id)
	if err != nil {
		return nil, err
	}
	if t.SessionID == nil || (t.Status != models.TaskFailed && t.Status != models.TaskTimedOut && t.Status != models.TaskCancelled) {
		return nil, ErrNotContinuable
	}
	updates := map[string]any{"status": models.TaskQueued, "status_reason": "continued", "attempts": 0, "lease_until": nil,
		"started_at": nil, "finished_at": nil, "result": "", "error": ""}
	// The workspace (worktree, uncommitted files) lives on the agent that ran the task.
	if t.AgentID == nil && t.AssignedAgentID != nil {
		updates["agent_id"] = *t.AssignedAgentID
	}
	if maxTurns > 0 {
		updates["max_turns"] = maxTurns
	}
	res := s.db.WithContext(ctx).Model(&models.Task{}).Where("id = ? AND organization_id = ? AND status = ?", t.ID, org, t.Status).Updates(updates)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrNotContinuable
	}
	s.audit.Best(ctx, audit.Entry{OrganizationID: org, ActorType: audit.ActorUser, ActorID: userID, Action: "task.continue", TargetType: "task", TargetID: t.ID,
		Metadata: map[string]any{"previous_session_id": *t.SessionID, "previous_status": t.Status, "max_turns": maxTurns}})
	if t, err = s.Get(ctx, org, id); err != nil {
		return nil, err
	}
	s.emit(ctx, t)
	s.bus.Wake(ctx)
	return t, nil
}

// ---- session hooks -------------------------------------------------------------------------------

// TaskProgress renews the lease and marks the task running.
func (s *Service) TaskProgress(ctx context.Context, taskID string) {
	now := time.Now().UTC()
	lease := now.Add(Lease)
	res := s.db.WithContext(ctx).Model(&models.Task{}).Where("id = ? AND status = ?", taskID, models.TaskAssigned).
		Updates(map[string]any{"status": models.TaskRunning, "lease_until": lease})
	if res.RowsAffected > 0 {
		if t, err := s.byID(ctx, taskID); err == nil {
			s.emit(ctx, t)
		}
		return
	}
	s.db.WithContext(ctx).Model(&models.Task{}).Where("id = ? AND status = ? AND (lease_until IS NULL OR lease_until < ?)", taskID, models.TaskRunning, now.Add(Lease-30*time.Second)).
		Update("lease_until", lease)
}

// TaskDone records the agent's outcome.
func (s *Service) TaskDone(ctx context.Context, taskID string, d proto.Done) {
	t, err := s.byID(ctx, taskID)
	if err != nil || models.TaskTerminal(t.Status) {
		return
	}
	status := models.TaskFailed
	switch d.Outcome {
	case proto.OutcomeSucceeded:
		status = models.TaskSucceeded
	case proto.OutcomeCancelled:
		status = models.TaskCancelled
	case proto.OutcomeTimedOut:
		status = models.TaskTimedOut
	}
	s.finish(ctx, t, status, d.Summary, d.Error)
}

// TaskLost handles a session that ended without an outcome: retry if attempts remain.
func (s *Service) TaskLost(ctx context.Context, taskID, reason string) {
	t, err := s.byID(ctx, taskID)
	if err != nil || models.TaskTerminal(t.Status) {
		return
	}
	s.requeueOrFail(ctx, t, "agent lost the task: "+reason)
}

func (s *Service) requeueOrFail(ctx context.Context, t *models.Task, reason string) {
	if t.SessionID != nil {
		s.db.WithContext(ctx).Model(&models.ChatSession{}).Where("id = ?", *t.SessionID).Update("status", models.SessionClosed)
	}
	if t.Attempts < t.MaxAttempts {
		s.db.WithContext(ctx).Model(t).Updates(map[string]any{"status": models.TaskQueued, "status_reason": reason,
			"assigned_agent_id": nil, "session_id": nil, "lease_until": nil})
		t.Status = models.TaskQueued
		s.audit.Best(ctx, audit.Entry{OrganizationID: t.OrganizationID, ActorType: audit.ActorSystem, Action: "task.requeue", TargetType: "task", TargetID: t.ID,
			Metadata: map[string]any{"reason": reason, "attempt": t.Attempts}})
		if nt, err := s.byID(ctx, t.ID); err == nil {
			s.emit(ctx, nt)
		}
		s.bus.Wake(ctx)
		return
	}
	s.finish(ctx, t, models.TaskFailed, "", reason)
}

func (s *Service) finish(ctx context.Context, t *models.Task, status, result, errMsg string) {
	now := time.Now().UTC()
	res := s.db.WithContext(ctx).Model(&models.Task{}).Where("id = ? AND status NOT IN ?", t.ID,
		[]string{models.TaskSucceeded, models.TaskFailed, models.TaskCancelled, models.TaskTimedOut}).
		Updates(map[string]any{"status": status, "result": result, "error": errMsg, "finished_at": now, "lease_until": nil})
	if res.RowsAffected == 0 {
		return
	}
	s.audit.Best(ctx, audit.Entry{OrganizationID: t.OrganizationID, ActorType: audit.ActorSystem, Action: "task." + status, TargetType: "task", TargetID: t.ID,
		Metadata: map[string]any{"error": errMsg}})
	nt, err := s.byID(ctx, t.ID)
	if err != nil {
		return
	}
	s.emit(ctx, nt)
	if status != models.TaskCancelled {
		s.notify.Send(fmt.Sprintf("Akili task %q %s (cost $%.4f)\n%s", nt.Title, status, nt.CostUSD, s.notify.Link("/tasks/"+nt.ID)))
		s.notify.Mail().TaskFinished(nt.OrganizationID, nt)
	}
	s.bus.Wake(ctx) // capacity freed
}

func (s *Service) byID(ctx context.Context, id string) (*models.Task, error) {
	var t models.Task
	if err := s.db.WithContext(ctx).First(&t, "id = ?", id).Error; err != nil {
		return nil, ErrNotFound
	}
	return &t, nil
}

func (s *Service) emit(ctx context.Context, t *models.Task) {
	s.bus.EmitData(ctx, t.OrganizationID, bus.Event{Type: EvTaskUpdated, TaskID: t.ID, SessionID: deref(t.SessionID)}, t)
}

// ---- leader loops --------------------------------------------------------------------------------

// Run drives dispatch, sweeping and schedules while this replica leads.
func (s *Service) Run(ctx context.Context) {
	wake := s.bus.SubscribeWake(ctx)
	dispatchTick := time.NewTicker(5 * time.Second)
	sweepTick := time.NewTicker(15 * time.Second)
	defer dispatchTick.Stop()
	defer sweepTick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-wake:
		case <-dispatchTick.C:
		case <-sweepTick.C:
			if s.elect.Leading() {
				s.sweep(ctx)
				s.runSchedules(ctx)
				s.hub.ExpireApprovals(ctx)
			}
			continue
		}
		if s.elect.Leading() {
			s.dispatch(ctx)
		}
	}
}

// dispatch assigns queued tasks to available agents.
func (s *Service) dispatch(ctx context.Context) {
	var queued []models.Task
	if err := s.db.WithContext(ctx).Where("status = ?", models.TaskQueued).Order("priority DESC, created_at").Limit(50).Find(&queued).Error; err != nil {
		logger.Warn("dispatch query failed", "error", err)
		return
	}
	if len(queued) == 0 {
		return
	}
	load := map[string]int{}
	var running []models.Task
	s.db.WithContext(ctx).Select("assigned_agent_id").Where("status IN ?", []string{models.TaskAssigned, models.TaskRunning}).Find(&running)
	for _, r := range running {
		if r.AssignedAgentID != nil {
			load[*r.AssignedAgentID]++
		}
	}
	agentsByOrg := map[string][]models.Agent{}
	present := map[string]bool{}
	for i := range queued {
		t := &queued[i]
		agents, ok := agentsByOrg[t.OrganizationID]
		if !ok {
			s.db.WithContext(ctx).Where("organization_id = ? AND status = ? AND NOT draining", t.OrganizationID, models.AgentOnline).Find(&agents)
			agentsByOrg[t.OrganizationID] = agents
			ids := make([]string, len(agents))
			for j := range agents {
				ids[j] = agents[j].ID
			}
			for id, p := range s.bus.PresentMany(ctx, ids) {
				present[id] = p
			}
		}
		agent := s.pick(t, agents, load, present)
		if agent == nil {
			reason := "waiting for an available agent"
			if t.AgentID != nil {
				reason = "waiting for the assigned agent to be online with free capacity"
			} else if len(t.Selector) > 0 {
				reason = "waiting for an online agent with labels " + strings.Join(t.Selector, ", ")
			}
			if t.StatusReason != reason {
				s.db.WithContext(ctx).Model(t).Update("status_reason", reason)
			}
			continue
		}
		if err := s.assign(ctx, t, agent); err != nil {
			logger.Warn("task assignment failed", "task", t.ID, "agent", agent.ID, "error", err)
			continue
		}
		load[agent.ID]++
	}
}

func (s *Service) pick(t *models.Task, agents []models.Agent, load map[string]int, present map[string]bool) *models.Agent {
	var candidates []*models.Agent
	for i := range agents {
		a := &agents[i]
		if t.AgentID != nil && a.ID != *t.AgentID {
			continue
		}
		if !a.HasLabels(t.Selector) || load[a.ID] >= a.MaxParallel || !present[a.ID] {
			continue
		}
		candidates = append(candidates, a)
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.SliceStable(candidates, func(i, j int) bool { return load[candidates[i].ID] < load[candidates[j].ID] })
	return candidates[0]
}

// assign claims the task, creates its session and starts it on the agent.
func (s *Service) assign(ctx context.Context, t *models.Task, agent *models.Agent) error {
	now := time.Now().UTC()
	lease := now.Add(Lease)
	var sess *models.ChatSession
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var locked models.Task
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			First(&locked, "id = ? AND status = ?", t.ID, models.TaskQueued).Error; err != nil {
			return err
		}
		sess = &models.ChatSession{Base: models.Base{ID: models.NewID("ses"), OrganizationID: t.OrganizationID}, AgentID: agent.ID, TaskID: &t.ID,
			Title: t.Title, Mode: proto.ModeTask, Status: models.SessionOpen, State: proto.StateIdle, CreatedBy: t.CreatedBy, LastActivityAt: &now,
			ProjectID: locked.ProjectID, Branch: locked.Branch}
		if err := tx.Create(sess).Error; err != nil {
			return err
		}
		// Only Continue leaves a session on a queued task (requeues clear it): carry its history over.
		if locked.SessionID != nil {
			if err := copyHistory(tx, *locked.SessionID, sess.ID); err != nil {
				return err
			}
		}
		updates := map[string]any{"status": models.TaskAssigned, "assigned_agent_id": agent.ID, "session_id": sess.ID,
			"attempts": gorm.Expr("attempts + 1"), "lease_until": lease, "status_reason": ""}
		if locked.StartedAt == nil {
			updates["started_at"] = now
		}
		return tx.Model(&models.Task{}).Where("id = ?", t.ID).Updates(updates).Error
	})
	if err != nil {
		return err
	}
	s.audit.Best(ctx, audit.Entry{OrganizationID: t.OrganizationID, ActorType: audit.ActorSystem, Action: "task.assign", TargetType: "task", TargetID: t.ID,
		Metadata: map[string]any{"agent_id": agent.ID, "session_id": sess.ID}})
	if nt, err := s.byID(ctx, t.ID); err == nil {
		s.emit(ctx, nt)
	}
	if err := s.bus.SendCommand(ctx, agent.ID, bus.Command{Type: bus.CmdStartTask, SessionID: sess.ID, TaskID: t.ID}); err != nil {
		// The agent vanished between selection and start; give the attempt back.
		s.db.WithContext(ctx).Model(&models.Task{}).Where("id = ?", t.ID).Updates(map[string]any{"status": models.TaskQueued,
			"assigned_agent_id": nil, "session_id": nil, "lease_until": nil, "attempts": gorm.Expr("attempts - 1")})
		s.db.WithContext(ctx).Model(sess).Update("status", models.SessionClosed)
		return err
	}
	return nil
}

func copyHistory(tx *gorm.DB, from, to string) error {
	var msgs []models.SessionMessage
	if err := tx.Where("session_id = ?", from).Order("id").Find(&msgs).Error; err != nil {
		return err
	}
	for i := range msgs {
		msgs[i].ID, msgs[i].SessionID = 0, to
	}
	if len(msgs) == 0 {
		return nil
	}
	return tx.CreateInBatches(msgs, 200).Error
}

// sweep requeues tasks whose lease expired and times out tasks past their deadline.
func (s *Service) sweep(ctx context.Context) {
	now := time.Now().UTC()
	var expired []models.Task
	s.db.WithContext(ctx).Where("status IN ? AND lease_until < ?", []string{models.TaskAssigned, models.TaskRunning}, now).Limit(100).Find(&expired)
	for i := range expired {
		t := &expired[i]
		// Still connected and holding the session: the task is alive, just quiet (e.g. waiting for an approval).
		if t.AssignedAgentID != nil && s.bus.Present(ctx, *t.AssignedAgentID) && s.waitingApproval(ctx, t) {
			s.db.WithContext(ctx).Model(t).Update("lease_until", now.Add(Lease))
			continue
		}
		s.requeueOrFail(ctx, t, "lease expired")
	}
	var active []models.Task
	s.db.WithContext(ctx).Where("status IN ? AND started_at IS NOT NULL AND timeout_sec > 0", []string{models.TaskAssigned, models.TaskRunning}).Limit(500).Find(&active)
	for i := range active {
		t := &active[i]
		if now.After(t.StartedAt.Add(time.Duration(t.TimeoutSec) * time.Second)) {
			if t.SessionID != nil && t.AssignedAgentID != nil {
				_ = s.bus.SendCommand(ctx, *t.AssignedAgentID, bus.Command{Type: bus.CmdInterrupt, SessionID: *t.SessionID})
				_ = s.hub.Close(ctx, t.OrganizationID, *t.SessionID, "")
			}
			s.finish(ctx, t, models.TaskTimedOut, "", fmt.Sprintf("exceeded timeout of %ds", t.TimeoutSec))
		}
	}
}

func (s *Service) waitingApproval(ctx context.Context, t *models.Task) bool {
	if t.SessionID == nil {
		return false
	}
	var n int64
	s.db.WithContext(ctx).Model(&models.Approval{}).Where("session_id = ? AND status = ?", *t.SessionID, models.ApprovalPending).Count(&n)
	return n > 0
}

// ---- schedules -----------------------------------------------------------------------------------

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// NextRun validates a cron expression and returns its next run after now.
func NextRun(expr string, after time.Time) (time.Time, error) {
	sched, err := cronParser.Parse(expr)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid cron expression: %w", err)
	}
	return sched.Next(after), nil
}

func (s *Service) runSchedules(ctx context.Context) {
	now := time.Now().UTC()
	var due []models.Schedule
	s.db.WithContext(ctx).Where("enabled AND next_run_at <= ?", now).Limit(50).Find(&due)
	for i := range due {
		sc := &due[i]
		next, err := NextRun(sc.Cron, now)
		if err != nil {
			s.db.WithContext(ctx).Model(sc).Update("enabled", false)
			continue
		}
		// Advance first, conditionally, so two leaders during a handover cannot both fire it.
		res := s.db.WithContext(ctx).Model(&models.Schedule{}).Where("id = ? AND next_run_at = ?", sc.ID, sc.NextRunAt).
			Updates(map[string]any{"next_run_at": next, "last_run_at": now})
		if res.RowsAffected == 0 {
			continue
		}
		if _, err := s.Fire(ctx, sc); err != nil {
			logger.Warn("schedule failed to create task", "schedule", sc.ID, "error", err)
		}
	}
}

// Fire creates a task from a schedule's template.
func (s *Service) Fire(ctx context.Context, sc *models.Schedule) (*models.Task, error) {
	tpl := sc.Template
	id := sc.ID
	return s.Create(ctx, sc.OrganizationID, "", Input{Title: tpl.Title, Goal: tpl.Goal, AgentID: tpl.AgentID, Selector: tpl.Selector,
		Priority: tpl.Priority, Autonomy: tpl.Autonomy, BudgetUSD: tpl.BudgetUSD, MaxTurns: tpl.MaxTurns, TimeoutSec: tpl.TimeoutSec,
		MaxAttempts: tpl.MaxAttempts, ScheduleID: &id, ProjectID: tpl.ProjectID})
}

func firstLine(s string, n int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > n {
		s = s[:n] + "…"
	}
	return s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
