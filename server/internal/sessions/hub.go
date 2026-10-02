// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package sessions owns chat and task sessions: what an agent is told when a session opens, and
// what happens to every frame it sends back — persistence, the authoritative policy check on each
// tool request, approvals, audit and the live event feed.
package sessions

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/audit"
	"github.com/goakili/akili/server/internal/bus"
	"github.com/goakili/akili/server/internal/crypto"
	"github.com/goakili/akili/server/internal/models"
	"github.com/goakili/akili/server/internal/notify"
	"github.com/jkaninda/logger"
	"gorm.io/gorm"
)

// Event types emitted to browsers.
const (
	EvSessionCreated   = "session.created"
	EvSessionState     = "session.state"
	EvSessionClosed    = "session.closed"
	EvDelta            = "assistant.delta"
	EvMessage          = "message"
	EvToolRequest      = "tool.request"
	EvToolResult       = "tool.result"
	EvApprovalCreated  = "approval.created"
	EvApprovalResolved = "approval.resolved"
	EvError            = "session.error"
	EvUsage            = "usage"
)

// ApprovalTTL is how long a pending approval stays valid.
const ApprovalTTL = 30 * time.Minute

// Output kept in the audit log and event timeline per tool result.
const maxRecordedOutput = 4000

// RemoteTools runs tools that execute on the control plane and builds project workspaces specs.
type RemoteTools interface {
	RunRemote(ctx context.Context, sess *models.ChatSession, tool string, input json.RawMessage) proto.RemoteResult
	Spec(ctx context.Context, sess *models.ChatSession, agent *models.Agent) (*proto.ProjectSpec, *models.Project, error)
}

// TaskHooks lets the task service follow its sessions without an import cycle.
type TaskHooks interface {
	TaskProgress(ctx context.Context, taskID string)
	TaskDone(ctx context.Context, taskID string, done proto.Done)
	TaskLost(ctx context.Context, taskID string, reason string)
}

// Hub is the session service.
type Hub struct {
	db         *gorm.DB
	bus        *bus.Bus
	audit      *audit.Logger
	notify     *notify.Notifier
	signingKey ed25519.PrivateKey
	tasks      TaskHooks
	remote     RemoteTools
	runners    map[string]RemoteRunner
	lessons    func(ctx context.Context, org, agentID string) []string
	mcpTools   func(ctx context.Context, org string) []proto.DynamicTool
	mcpRun     RemoteRunner
}

// SetRemoteTools wires the coder service.
func (h *Hub) SetRemoteTools(r RemoteTools) { h.remote = r }

// SetMCP connects MCP tools: tools lists an organization's usable ones; run executes a call.
func (h *Hub) SetMCP(tools func(ctx context.Context, org string) []proto.DynamicTool, run RemoteRunner) {
	h.mcpTools, h.mcpRun = tools, run
}

// SetLessons supplies the approved lessons added to new sessions' system prompts.
func (h *Hub) SetLessons(f func(ctx context.Context, org, agentID string) []string) { h.lessons = f }

// New returns a hub.
func New(db *gorm.DB, b *bus.Bus, a *audit.Logger, n *notify.Notifier, key ed25519.PrivateKey) *Hub {
	return &Hub{db: db, bus: b, audit: a, notify: n, signingKey: key}
}

// SetTaskHooks wires the task service.
func (h *Hub) SetTaskHooks(t TaskHooks) { h.tasks = t }

// SigningPublicKey is pinned by agents at enrollment.
func (h *Hub) SigningPublicKey() ed25519.PublicKey { return h.signingKey.Public().(ed25519.PublicKey) }

// ErrNotFound is returned for unknown or foreign sessions.
var ErrNotFound = errors.New("session not found")

// Create opens a session row.
func (h *Hub) Create(ctx context.Context, org, agentID, userID, title, mode string, taskID *string, projectID ...string) (*models.ChatSession, error) {
	if title == "" {
		title = "Session " + time.Now().UTC().Format("2006-01-02 15:04")
	}
	now := time.Now().UTC()
	s := &models.ChatSession{Base: models.Base{ID: models.NewID("ses"), OrganizationID: org}, AgentID: agentID, TaskID: taskID,
		Title: title, Mode: mode, Status: models.SessionOpen, State: proto.StateIdle, CreatedBy: userID, LastActivityAt: &now}
	if len(projectID) > 0 && projectID[0] != "" {
		s.ProjectID = &projectID[0]
		s.Branch = "akili/" + s.ID
	}
	if err := h.db.WithContext(ctx).Create(s).Error; err != nil {
		return nil, err
	}
	h.bus.EmitData(ctx, org, bus.Event{Type: EvSessionCreated, SessionID: s.ID, AgentID: agentID}, s)
	return s, nil
}

// Get loads a session within an organization.
func (h *Hub) Get(ctx context.Context, org, id string) (*models.ChatSession, error) {
	var s models.ChatSession
	if err := h.db.WithContext(ctx).First(&s, "id = ? AND organization_id = ?", id, org).Error; err != nil {
		return nil, ErrNotFound
	}
	return &s, nil
}

// BuildOpen assembles the session.open message for an agent. It fixes the system prompt and tool
// set on first open, so a reconnect replays the conversation against exactly the same prefix.
func (h *Hub) BuildOpen(ctx context.Context, sessionID, agentID string) (proto.SessionOpen, error) {
	var open proto.SessionOpen
	var s models.ChatSession
	if err := h.db.WithContext(ctx).First(&s, "id = ? AND agent_id = ?", sessionID, agentID).Error; err != nil {
		return open, ErrNotFound
	}
	if s.Status != models.SessionOpen {
		return open, fmt.Errorf("session %s is closed", sessionID)
	}
	var agent models.Agent
	if err := h.db.WithContext(ctx).Preload("Skills").First(&agent, "id = ?", agentID).Error; err != nil {
		return open, err
	}
	pol, err := h.agentPolicy(ctx, &agent)
	if err != nil {
		return open, err
	}
	signed, err := proto.SignPolicy(pol, h.signingKey)
	if err != nil {
		return open, err
	}
	var spec *proto.ProjectSpec
	var project *models.Project
	if s.ProjectID != nil {
		if h.remote == nil {
			return open, errors.New("coding sessions are not available on this control plane")
		}
		if spec, project, err = h.remote.Spec(ctx, &s, &agent); err != nil {
			return open, fmt.Errorf("project: %w", err)
		}
	}
	if s.System == "" {
		s.ToolNames = offeredTools(pol, spec)
		if h.mcpTools != nil {
			for _, t := range h.mcpTools(ctx, s.OrganizationID) {
				if pol.AllowsTool(t.Name) {
					s.ToolNames = append(s.ToolNames, t.Name)
				}
			}
		}
		var lessons []string
		if h.lessons != nil {
			lessons = h.lessons(ctx, s.OrganizationID, agent.ID)
		}
		s.System = buildSystemPrompt(&agent, agent.Skills, s.Mode, s.ToolNames, project, spec, lessons)
		// Struct update with Select: map updates bypass the JSON serializer on tool_names.
		if err := h.db.WithContext(ctx).Model(&s).Select("system", "tool_names").Updates(&models.ChatSession{System: s.System, ToolNames: s.ToolNames}).Error; err != nil {
			return open, err
		}
	}
	var history []models.SessionMessage
	if err := h.db.WithContext(ctx).Where("session_id = ?", s.ID).Order("id").Find(&history).Error; err != nil {
		return open, err
	}
	open = proto.SessionOpen{
		SessionID: s.ID, Mode: s.Mode, System: s.System, Policy: signed, Autonomy: agent.Autonomy,
		MaxTurns: 40, ToolNames: s.ToolNames, Project: spec,
	}
	// MCP tools travel with their current risk (an admin may have changed it since the session began).
	for _, name := range s.ToolNames {
		if proto.IsMCPTool(name) {
			if t, ok := proto.LookupTool(name); ok {
				open.MCPTools = append(open.MCPTools, proto.DynamicTool{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema, Risk: t.Risk})
			}
		}
	}
	for _, m := range history {
		open.History = append(open.History, proto.Message{Role: m.Role, Content: m.Content})
	}
	if s.TaskID != nil {
		var t models.Task
		if err := h.db.WithContext(ctx).First(&t, "id = ?", *s.TaskID).Error; err != nil {
			return open, err
		}
		open.TaskID = t.ID
		open.Autonomy = minAutonomy(t.Autonomy, agent.Autonomy)
		if t.MaxTurns > 0 {
			open.MaxTurns = t.MaxTurns
		}
		if len(history) == 0 {
			open.Goal = t.Goal
		}
		if t.TimeoutSec > 0 && t.StartedAt != nil {
			d := t.StartedAt.Add(time.Duration(t.TimeoutSec) * time.Second)
			open.Deadline = &d
		}
	}
	return open, nil
}

func minAutonomy(a, b proto.Autonomy) proto.Autonomy {
	if a < b {
		return a
	}
	return b
}

// SignedAgentPolicy returns an agent's current policy, signed for the agent to verify.
func (h *Hub) SignedAgentPolicy(ctx context.Context, agentID string) (proto.Policy, proto.SignedPolicy, error) {
	var agent models.Agent
	if err := h.db.WithContext(ctx).First(&agent, "id = ?", agentID).Error; err != nil {
		return proto.Policy{}, proto.SignedPolicy{}, err
	}
	pol, err := h.agentPolicy(ctx, &agent)
	if err != nil {
		return pol, proto.SignedPolicy{}, err
	}
	signed, err := proto.SignPolicy(pol, h.signingKey)
	return pol, signed, err
}

// agentPolicy returns the agent's policy, or an empty (deny-all) policy when none is bound.
func (h *Hub) agentPolicy(ctx context.Context, agent *models.Agent) (proto.Policy, error) {
	if agent.PolicyID == nil {
		return proto.Policy{Name: "none"}, nil
	}
	var p models.Policy
	if err := h.db.WithContext(ctx).First(&p, "id = ? AND organization_id = ?", *agent.PolicyID, agent.OrganizationID).Error; err != nil {
		return proto.Policy{}, fmt.Errorf("agent policy: %w", err)
	}
	doc := p.Document
	doc.Name, doc.Version = p.Name, p.Version
	return doc, nil
}

// HandleFrame processes one envelope an agent sent on a session stream. A non-nil decision must be
// sent back to the agent.
func (h *Hub) HandleFrame(ctx context.Context, agentID, sessionID string, env proto.Envelope) (*proto.ToolDecision, error) {
	var s models.ChatSession
	if err := h.db.WithContext(ctx).First(&s, "id = ? AND agent_id = ?", sessionID, agentID).Error; err != nil {
		return nil, ErrNotFound
	}
	h.touch(ctx, &s)
	switch env.Type {
	case proto.TypeDelta:
		var d proto.Delta
		if env.Decode(&d) == nil {
			h.bus.EmitData(ctx, s.OrganizationID, bus.Event{Type: EvDelta, SessionID: s.ID, AgentID: agentID}, d)
		}
	case proto.TypeStatus:
		var st proto.Status
		if err := env.Decode(&st); err != nil {
			return nil, err
		}
		h.db.WithContext(ctx).Model(&s).Update("state", st.State)
		h.bus.EmitData(ctx, s.OrganizationID, bus.Event{Type: EvSessionState, SessionID: s.ID, AgentID: agentID}, st)
		if s.TaskID != nil && h.tasks != nil {
			h.tasks.TaskProgress(ctx, *s.TaskID)
		}
	case proto.TypeMessageAppend:
		var m proto.MessageAppend
		if err := env.Decode(&m); err != nil {
			return nil, err
		}
		if m.Message.Role != proto.RoleUser && m.Message.Role != proto.RoleAssistant {
			return nil, fmt.Errorf("invalid message role %q", m.Message.Role)
		}
		row := models.SessionMessage{SessionID: s.ID, Role: m.Message.Role, Content: m.Message.Content}
		if err := h.db.WithContext(ctx).Create(&row).Error; err != nil {
			return nil, err
		}
		h.bus.EmitData(ctx, s.OrganizationID, bus.Event{Type: EvMessage, SessionID: s.ID, AgentID: agentID}, row)
	case proto.TypeToolRequest:
		var req proto.ToolRequest
		if err := env.Decode(&req); err != nil {
			return nil, err
		}
		d := h.authorize(ctx, &s, req)
		if d.Effect == proto.EffectAllow && isRemote(req.Tool) {
			// Remote tools (forge, Miabi) can take minutes; answer when they finish so the session
			// stream keeps flowing.
			h.completeRemote(&s, d, req.Tool, req.Input)
			return nil, nil
		}
		return &d, nil
	case proto.TypeToolResult:
		var r proto.ToolResult
		if err := env.Decode(&r); err != nil {
			return nil, err
		}
		h.recordResult(ctx, &s, r)
		if r.ChangeID != "" {
			h.recordChangeResult(ctx, &s, r)
		}
	case proto.TypeChangeUpdate:
		var u proto.ChangeUpdate
		if err := env.Decode(&u); err != nil {
			return nil, err
		}
		h.changeUpdate(ctx, &s, u)
	case proto.TypeDone:
		var d proto.Done
		if err := env.Decode(&d); err != nil {
			return nil, err
		}
		h.addEvent(ctx, &s, "done", d)
		if s.TaskID != nil && h.tasks != nil {
			h.tasks.TaskDone(ctx, *s.TaskID, d)
		}
		h.close(ctx, &s)
	case proto.TypeError:
		var e proto.Error
		_ = env.Decode(&e)
		h.addEvent(ctx, &s, "error", e)
	default:
		logger.Debug("ignoring unknown session frame", "type", env.Type, "session", sessionID)
	}
	return nil, nil
}

// authorize is the authoritative policy check. It never trusts the agent's view of risk: the tool,
// its risk and what it touches all come from the shared catalog and the current policy.
func (h *Hub) authorize(ctx context.Context, s *models.ChatSession, req proto.ToolRequest) proto.ToolDecision {
	out := proto.ToolDecision{RequestID: req.RequestID}
	var agent models.Agent
	if err := h.db.WithContext(ctx).First(&agent, "id = ?", s.AgentID).Error; err != nil {
		out.Effect, out.Reason = proto.EffectDeny, "agent not found"
		return out
	}
	var org models.Organization
	h.db.WithContext(ctx).First(&org, "id = ?", s.OrganizationID)

	var d proto.Decision
	var plan *proto.ChangePlan
	autonomy := agent.Autonomy
	if s.TaskID != nil {
		var t models.Task
		if h.db.WithContext(ctx).Select("autonomy").First(&t, "id = ?", *s.TaskID).Error == nil {
			autonomy = minAutonomy(t.Autonomy, agent.Autonomy)
		}
	}
	switch {
	case org.KillSwitch:
		d = proto.Decision{Effect: proto.EffectDeny, Reason: "the organization kill switch is engaged"}
	case agent.Status == models.AgentRevoked:
		d = proto.Decision{Effect: proto.EffectDeny, Reason: "agent is revoked"}
	case isProjectTool(req.Tool) && s.ProjectID == nil:
		d = proto.Decision{Effect: proto.EffectDeny, Reason: req.Tool + " needs a session bound to a project"}
	default:
		pol, err := h.agentPolicy(ctx, &agent)
		if err != nil {
			d = proto.Decision{Effect: proto.EffectDeny, Reason: "policy unavailable"}
		} else {
			base := sessionBase(s, &agent, h)
			switch {
			case req.Tool == proto.ToolChangeRun:
				d, plan = h.evaluateChange(pol, autonomy, agent.Facts.Workdir, base, req)
			case req.ChangeID != "":
				d = proto.EvaluateAt(pol, autonomy, agent.Facts.Workdir, base, proto.Call{Tool: req.Tool, Input: req.Input})
				if d.Effect != proto.EffectDeny {
					// Policy denials still win; otherwise the human approval of the plan covers the call.
					if ok, reason := h.authorizeChangeCall(ctx, s, req); ok {
						d.Effect, d.Reason = proto.EffectAllow, reason
					} else {
						d.Effect, d.Reason = proto.EffectDeny, reason
					}
				}
			default:
				d = proto.EvaluateAt(pol, autonomy, agent.Facts.Workdir, base, proto.Call{Tool: req.Tool, Input: req.Input})
			}
		}
	}

	inputHash := crypto.SHA256Hex(append([]byte(req.Tool+"\n"), req.Input...))
	meta := map[string]any{
		"session_id": s.ID, "request_id": req.RequestID, "tool": req.Tool, "risk": d.Risk.String(),
		"decision": d.Effect, "reason": d.Reason, "input_hash": inputHash, "input": truncateJSON(req.Input, 2000),
		"autonomy": int(autonomy),
	}
	if req.ChangeID != "" {
		meta["change_id"], meta["phase"] = req.ChangeID, req.Phase
	}
	if s.TaskID != nil {
		meta["task_id"] = *s.TaskID
	}
	// Audit before anything runs. If the audit trail cannot be written, nothing runs.
	if err := h.audit.Record(ctx, audit.Entry{OrganizationID: s.OrganizationID, ActorType: audit.ActorAgent, ActorID: agent.ID,
		Action: "tool.request", TargetType: "session", TargetID: s.ID, Metadata: meta}); err != nil {
		logger.Error("audit unavailable; denying tool call", "error", err)
		d.Effect, d.Reason = proto.EffectDeny, "audit trail unavailable"
	}
	out.Effect, out.Reason = d.Effect, d.Reason

	if d.Effect == proto.EffectApprove {
		ap := models.Approval{Base: models.Base{ID: models.NewID("apr"), OrganizationID: s.OrganizationID}, SessionID: s.ID,
			TaskID: s.TaskID, AgentID: agent.ID, RequestID: req.RequestID, Tool: req.Tool, Input: req.Input, InputHash: inputHash,
			Risk: d.Risk.String(), Reason: d.Reason, Status: models.ApprovalPending, ExpiresAt: time.Now().UTC().Add(ApprovalTTL)}
		err := h.db.WithContext(ctx).Create(&ap).Error
		if err == nil && plan != nil {
			err = h.createChange(ctx, s, &ap, plan, d.Risk)
		}
		if err != nil {
			out.Effect, out.Reason = proto.EffectDeny, "could not create approval"
		} else {
			out.ApprovalID = ap.ID
			h.bus.EmitData(ctx, s.OrganizationID, bus.Event{Type: EvApprovalCreated, SessionID: s.ID, AgentID: agent.ID, TaskID: deref(s.TaskID)}, ap)
			h.notify.Send(fmt.Sprintf("Akili: agent %q requests approval for %s (%s risk): %s\n%s",
				agent.Name, req.Tool, ap.Risk, truncateJSON(req.Input, 300), h.notify.Link("/approvals")))
			h.notify.Mail().ApprovalRequested(s.OrganizationID, &ap, agent.Name)
		}
	}
	h.addEvent(ctx, s, EvToolRequest, map[string]any{
		"request_id": req.RequestID, "tool_use_id": req.ToolUseID, "tool": req.Tool, "input": req.Input,
		"effect": out.Effect, "reason": out.Reason, "risk": d.Risk.String(), "approval_id": out.ApprovalID,
		"change_id": req.ChangeID, "phase": req.Phase,
	})
	return out
}

// runRemote executes an allowed tool on the control plane when the catalog marks it remote.
func (h *Hub) runRemote(ctx context.Context, s *models.ChatSession, tool string, input json.RawMessage) *proto.RemoteResult {
	spec, ok := proto.LookupTool(tool)
	if !ok || !spec.Remote {
		return nil
	}
	var res proto.RemoteResult
	switch run, ok := h.runners[tool]; {
	case proto.IsMCPTool(tool) && h.mcpRun != nil:
		res = h.mcpRun(ctx, s, tool, input)
	case ok:
		res = run(ctx, s, tool, input)
	case h.remote != nil:
		res = h.remote.RunRemote(ctx, s, tool, input)
	default:
		return &proto.RemoteResult{Output: "error: remote tools are not available", IsError: true}
	}
	// Remote output (Miabi logs, forge responses) enters the model's context: strip credentials.
	res.Output = proto.Redact(res.Output)
	return &res
}

// RemoteRunner executes one remote tool.
type RemoteRunner func(ctx context.Context, s *models.ChatSession, tool string, input json.RawMessage) proto.RemoteResult

// AddRemoteRunner registers the executor of remote tools (e.g. the Miabi service).
func (h *Hub) AddRemoteRunner(tools []string, run RemoteRunner) {
	if h.runners == nil {
		h.runners = map[string]RemoteRunner{}
	}
	for _, t := range tools {
		h.runners[t] = run
	}
}

// remoteTimeout bounds one remote tool call (a Miabi deploy waits for the deployment).
const remoteTimeout = 20 * time.Minute

// completeRemote runs an allowed remote tool in the background and sends the final decision, with
// its result, to the agent.
func (h *Hub) completeRemote(s *models.ChatSession, d proto.ToolDecision, tool string, input json.RawMessage) {
	sess := *s
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), remoteTimeout)
		defer cancel()
		d.Result = h.runRemote(ctx, &sess, tool, input)
		if err := h.bus.SendCommandData(ctx, sess.AgentID, bus.Command{Type: bus.CmdToolDecision, SessionID: sess.ID}, d); err != nil {
			logger.Warn("remote tool finished but the agent is gone", "tool", tool, "session", sess.ID, "error", err)
		}
	}()
}

func isRemote(tool string) bool {
	spec, ok := proto.LookupTool(tool)
	return ok && spec.Remote
}

func isProjectTool(tool string) bool {
	spec, ok := proto.LookupTool(tool)
	return ok && spec.Project
}

// sessionBase is where relative paths resolve: the project worktree in a project session.
func sessionBase(s *models.ChatSession, agent *models.Agent, h *Hub) string {
	if s.ProjectID == nil || h.remote == nil {
		return agent.Facts.Workdir
	}
	var p models.Project
	if h.db.Select("slug").First(&p, "id = ?", *s.ProjectID).Error != nil {
		return agent.Facts.Workdir
	}
	return proto.ProjectDir(agent.Facts.Workdir, p.Slug, s.Branch)
}

func (h *Hub) recordResult(ctx context.Context, s *models.ChatSession, r proto.ToolResult) {
	out := r.Output
	if len(out) > maxRecordedOutput {
		out = out[:maxRecordedOutput] + "…"
	}
	h.audit.Best(ctx, audit.Entry{OrganizationID: s.OrganizationID, ActorType: audit.ActorAgent, ActorID: s.AgentID,
		Action: "tool.result", TargetType: "session", TargetID: s.ID, Metadata: map[string]any{
			"request_id": r.RequestID, "tool": r.Tool, "is_error": r.IsError, "duration_ms": r.DurationMs,
			"output_sha256": crypto.SHA256Hex([]byte(r.Output)), "output_bytes": len(r.Output),
		}})
	r.Output = out
	h.addEvent(ctx, s, EvToolResult, r)
}

func (h *Hub) addEvent(ctx context.Context, s *models.ChatSession, typ string, payload any) {
	b, _ := json.Marshal(payload)
	ev := models.SessionEvent{SessionID: s.ID, Type: typ, Payload: b}
	if err := h.db.WithContext(ctx).Create(&ev).Error; err != nil {
		logger.Warn("session event not persisted", "type", typ, "error", err)
	}
	h.bus.Emit(ctx, s.OrganizationID, bus.Event{Type: typ, SessionID: s.ID, AgentID: s.AgentID, TaskID: deref(s.TaskID), Data: b})
}

func (h *Hub) touch(ctx context.Context, s *models.ChatSession) {
	now := time.Now().UTC()
	if s.LastActivityAt == nil || now.Sub(*s.LastActivityAt) > 5*time.Second {
		h.db.WithContext(ctx).Model(s).Update("last_activity_at", now)
	}
}

func (h *Hub) close(ctx context.Context, s *models.ChatSession) {
	h.db.WithContext(ctx).Model(s).Updates(map[string]any{"status": models.SessionClosed, "state": proto.StateIdle})
	h.bus.Emit(ctx, s.OrganizationID, bus.Event{Type: EvSessionClosed, SessionID: s.ID, AgentID: s.AgentID})
}

// SessionLost is called when one session stream ends without a done frame while the tunnel is up
// (the agent crashed the session or refused it). Chat sessions go idle and reopen on the next
// message; tasks are handed back to the task service to retry.
func (h *Hub) SessionLost(ctx context.Context, sessionID string, reason string) {
	h.sessionEnded(ctx, sessionID, reason, true)
}

// SessionDetached is called when the whole tunnel dropped (agent disconnected, or its replica went
// away). A task keeps its lease: the agent resumes it on reconnect, to any replica, and the sweep
// requeues it only if the agent does not come back in time.
func (h *Hub) SessionDetached(ctx context.Context, sessionID string, reason string) {
	h.sessionEnded(ctx, sessionID, reason, false)
}

func (h *Hub) sessionEnded(ctx context.Context, sessionID string, reason string, taskLost bool) {
	var s models.ChatSession
	if err := h.db.WithContext(ctx).First(&s, "id = ?", sessionID).Error; err != nil {
		return
	}
	h.db.WithContext(ctx).Model(&s).Update("state", proto.StateIdle)
	h.bus.EmitData(ctx, s.OrganizationID, bus.Event{Type: EvSessionState, SessionID: s.ID, AgentID: s.AgentID},
		proto.Status{State: proto.StateIdle, Detail: reason})
	// Pending approvals for a stream that no longer exists can never be delivered.
	var stale []models.Approval
	h.db.WithContext(ctx).Where("session_id = ? AND status = ?", s.ID, models.ApprovalPending).Find(&stale)
	for i := range stale {
		ap := &stale[i]
		res := h.db.WithContext(ctx).Model(&models.Approval{}).Where("id = ? AND status = ?", ap.ID, models.ApprovalPending).
			Updates(map[string]any{"status": models.ApprovalExpired, "note": "session stream ended: " + reason})
		if res.RowsAffected > 0 {
			ap.Status, ap.Note = models.ApprovalExpired, "session stream ended: "+reason
			h.addEvent(ctx, &s, EvApprovalResolved, ap)
			h.resolveChange(ctx, ap.ID, proto.ChangeExpired)
		}
	}
	if taskLost && s.Status == models.SessionOpen && s.TaskID != nil && h.tasks != nil {
		h.tasks.TaskLost(ctx, *s.TaskID, reason)
	}
}

// PostUserMessage delivers operator input to the agent holding the session.
func (h *Hub) PostUserMessage(ctx context.Context, org, sessionID, userID, text string, attachments ...string) error {
	s, err := h.Get(ctx, org, sessionID)
	if err != nil {
		return err
	}
	if s.Status != models.SessionOpen {
		return errors.New("session is closed")
	}
	if s.Mode == proto.ModeTask && s.TaskID != nil {
		var t models.Task
		if h.db.WithContext(ctx).Select("status").First(&t, "id = ?", *s.TaskID).Error == nil && models.TaskTerminal(t.Status) {
			return errors.New("task has finished")
		}
	}
	images, err := h.imageRefs(ctx, s, attachments)
	if err != nil {
		return err
	}
	h.audit.Best(ctx, audit.Entry{OrganizationID: org, ActorType: audit.ActorUser, ActorID: userID, Action: "session.message",
		TargetType: "session", TargetID: s.ID, Metadata: map[string]any{"chars": len(text), "images": len(images)}})
	return h.bus.SendCommandData(ctx, s.AgentID, bus.Command{Type: bus.CmdUserMessage, SessionID: s.ID},
		proto.UserMessage{Text: text, UserID: userID, Images: images})
}

// Interrupt stops the agent's current turn.
func (h *Hub) Interrupt(ctx context.Context, org, sessionID, userID string) error {
	s, err := h.Get(ctx, org, sessionID)
	if err != nil {
		return err
	}
	h.audit.Best(ctx, audit.Entry{OrganizationID: org, ActorType: audit.ActorUser, ActorID: userID, Action: "session.interrupt", TargetType: "session", TargetID: s.ID})
	return h.bus.SendCommand(ctx, s.AgentID, bus.Command{Type: bus.CmdInterrupt, SessionID: s.ID})
}

// Close ends a session. The agent is told if connected; closing succeeds either way.
func (h *Hub) Close(ctx context.Context, org, sessionID, userID string) error {
	s, err := h.Get(ctx, org, sessionID)
	if err != nil {
		return err
	}
	_ = h.bus.SendCommand(ctx, s.AgentID, bus.Command{Type: bus.CmdClose, SessionID: s.ID})
	h.close(ctx, s)
	h.audit.Best(ctx, audit.Entry{OrganizationID: org, ActorType: audit.ActorUser, ActorID: userID, Action: "session.close", TargetType: "session", TargetID: s.ID})
	return nil
}

// ErrApprovalClosed is returned when an approval is no longer pending.
var ErrApprovalClosed = errors.New("approval is no longer pending")

// Decide approves or denies a pending approval and tells the waiting agent.
func (h *Hub) Decide(ctx context.Context, org, approvalID, userID string, allow bool, note string) (*models.Approval, error) {
	var ap models.Approval
	if err := h.db.WithContext(ctx).First(&ap, "id = ? AND organization_id = ?", approvalID, org).Error; err != nil {
		return nil, ErrNotFound
	}
	now := time.Now().UTC()
	status := models.ApprovalDenied
	if allow {
		status = models.ApprovalApproved
	}
	if ap.Status == models.ApprovalPending && now.After(ap.ExpiresAt) {
		h.expire(ctx, &ap)
		return nil, ErrApprovalClosed
	}
	// Conditional update: two operators racing on the same approval cannot both decide it.
	res := h.db.WithContext(ctx).Model(&models.Approval{}).Where("id = ? AND status = ?", ap.ID, models.ApprovalPending).
		Updates(map[string]any{"status": status, "decided_by": userID, "decided_at": now, "note": note})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrApprovalClosed
	}
	ap.Status, ap.DecidedBy, ap.DecidedAt, ap.Note = status, &userID, &now, note
	if err := h.audit.Record(ctx, audit.Entry{OrganizationID: org, ActorType: audit.ActorUser, ActorID: userID,
		Action: "approval." + status, TargetType: "approval", TargetID: ap.ID, Metadata: map[string]any{
			"tool": ap.Tool, "input_hash": ap.InputHash, "session_id": ap.SessionID, "note": note}}); err != nil {
		// Never deliver an approval that is not on the record.
		h.db.WithContext(ctx).Model(&models.Approval{}).Where("id = ?", ap.ID).Updates(map[string]any{"status": models.ApprovalPending, "decided_by": nil, "decided_at": nil})
		return nil, fmt.Errorf("audit unavailable: %w", err)
	}
	effect := proto.EffectDeny
	reason := "denied by operator"
	if allow {
		effect, reason = proto.EffectAllow, "approved by operator"
	}
	if note != "" {
		reason += ": " + note
	}
	var s models.ChatSession
	h.db.WithContext(ctx).First(&s, "id = ?", ap.SessionID)
	h.addEvent(ctx, &s, EvApprovalResolved, ap)
	decision := proto.ToolDecision{RequestID: ap.RequestID, Effect: effect, Reason: reason, ApprovalID: ap.ID}
	if allow && isRemote(ap.Tool) {
		h.completeRemote(&s, decision, ap.Tool, ap.Input)
		return &ap, nil
	}
	if ap.Tool == proto.ToolChangeRun {
		st := proto.ChangeDenied
		if allow {
			st = proto.ChangeApproved
		}
		if ch := h.resolveChange(ctx, ap.ID, st); ch != nil && allow {
			decision.ChangeID = ch.ID
		}
	}
	if err := h.bus.SendCommandData(ctx, ap.AgentID, bus.Command{Type: bus.CmdToolDecision, SessionID: ap.SessionID}, decision); err != nil {
		logger.Warn("approval recorded but agent is offline", "approval", ap.ID, "error", err)
	}
	return &ap, nil
}

// ExpireApprovals denies approvals past their deadline.
func (h *Hub) ExpireApprovals(ctx context.Context) {
	var aps []models.Approval
	h.db.WithContext(ctx).Where("status = ? AND expires_at < ?", models.ApprovalPending, time.Now().UTC()).Limit(100).Find(&aps)
	for i := range aps {
		h.expire(ctx, &aps[i])
	}
}

func (h *Hub) expire(ctx context.Context, ap *models.Approval) {
	res := h.db.WithContext(ctx).Model(&models.Approval{}).Where("id = ? AND status = ?", ap.ID, models.ApprovalPending).
		Update("status", models.ApprovalExpired)
	if res.RowsAffected == 0 {
		return
	}
	ap.Status = models.ApprovalExpired
	h.resolveChange(ctx, ap.ID, proto.ChangeExpired)
	h.audit.Best(ctx, audit.Entry{OrganizationID: ap.OrganizationID, ActorType: audit.ActorSystem, Action: "approval.expired", TargetType: "approval", TargetID: ap.ID})
	var s models.ChatSession
	if h.db.WithContext(ctx).First(&s, "id = ?", ap.SessionID).Error == nil {
		h.addEvent(ctx, &s, EvApprovalResolved, ap)
	}
	_ = h.bus.SendCommandData(ctx, ap.AgentID, bus.Command{Type: bus.CmdToolDecision, SessionID: ap.SessionID},
		proto.ToolDecision{RequestID: ap.RequestID, Effect: proto.EffectDeny, Reason: "approval expired", ApprovalID: ap.ID})
}

// RecordUsage adds a model call's cost to the session.
func (h *Hub) RecordUsage(ctx context.Context, s *models.ChatSession, in, out int, cost float64) {
	h.db.WithContext(ctx).Model(&models.ChatSession{}).Where("id = ?", s.ID).Updates(map[string]any{
		"input_tokens":  gorm.Expr("input_tokens + ?", in),
		"output_tokens": gorm.Expr("output_tokens + ?", out),
		"cost_usd":      gorm.Expr("cost_usd + ?", cost),
	})
	h.bus.EmitData(ctx, s.OrganizationID, bus.Event{Type: EvUsage, SessionID: s.ID, AgentID: s.AgentID},
		map[string]any{"input_tokens": in, "output_tokens": out, "cost_usd": cost})
}

func truncateJSON(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
