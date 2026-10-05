// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package runtime runs chat and task sessions on the agent: the model/tool loop, local policy
// enforcement, and the request/decision round-trip with the control plane for every tool call.
package runtime

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/goakili/akili/agent/internal/tools"
	"github.com/goakili/akili/proto"
	"github.com/jkaninda/logger"
)

// Completer is the model gateway.
type Completer interface {
	Complete(ctx context.Context, req proto.LLMRequest, onDelta func(kind, text string)) (*proto.LLMEvent, error)
}

// Conn is the session stream.
type Conn interface {
	Send(typ string, payload any) error
	Recv() (proto.Envelope, error)
	Close() error
}

// Config is what a session needs from the agent.
type Config struct {
	Workdir      string
	CPSigningKey ed25519.PublicKey
	Facts        func() proto.HostFacts
	// ShellEnv are extra KEY=value pairs for shell tools (operator-chosen pass-through, e.g. KUBECONFIG).
	ShellEnv []string
	// Protected are directories tools must never touch outside the workdir (the agent's state and key).
	Protected []string
	// GitRemote returns the local git proxy URL for a session (project sessions).
	GitRemote func(sessionID string) string
}

// Session is one live session stream.
type Session struct {
	cfg    Config
	conn   Conn
	llm    Completer
	open   proto.SessionOpen
	policy proto.Policy
	exec   *tools.Executor
	defs   []proto.ToolDef

	history []proto.Message

	mu        sync.Mutex
	pending   map[string]chan proto.ToolDecision
	cancelRun context.CancelFunc

	userCh chan proto.Message
	closed chan struct{}
}

// ErrBadPolicy means the policy bundle was not signed by the pinned control-plane key.
var ErrBadPolicy = errors.New("session policy failed signature verification")

// NewSession validates a session.open and prepares the session.
func NewSession(cfg Config, conn Conn, llm Completer, open proto.SessionOpen) (*Session, error) {
	pol, err := open.Policy.Verify(cfg.CPSigningKey)
	if err != nil {
		return nil, ErrBadPolicy
	}
	s := &Session{cfg: cfg, conn: conn, llm: llm, open: open, policy: pol, history: open.History,
		pending: map[string]chan proto.ToolDecision{}, userCh: make(chan proto.Message, 16), closed: make(chan struct{})}
	s.exec = &tools.Executor{Workdir: cfg.Workdir, Policy: pol, Facts: cfg.Facts, ShellEnv: cfg.ShellEnv, Protected: cfg.Protected}
	// MCP tools run on the control plane; registering them lets the local policy check see their risk.
	byServer := map[string][]proto.DynamicTool{}
	for _, t := range open.MCPTools {
		if parts := strings.SplitN(t.Name, "__", 3); len(parts) == 3 && proto.IsMCPTool(t.Name) {
			prefix := parts[0] + "__" + parts[1] + "__"
			byServer[prefix] = append(byServer[prefix], t)
		}
	}
	for prefix, ts := range byServer {
		proto.SetDynamicTools(prefix, ts)
	}
	for _, name := range open.ToolNames {
		if spec, ok := proto.LookupTool(name); ok {
			s.defs = append(s.defs, spec.Def())
		}
	}
	if s.open.MaxTurns <= 0 {
		s.open.MaxTurns = 40
	}
	return s, nil
}

// ID returns the session id.
func (s *Session) ID() string { return s.open.SessionID }

// Run drives the session until the stream closes or a task finishes.
func (s *Session) Run(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go s.read(cancel)
	s.repairHistory()

	if spec := s.open.Project; spec != nil {
		s.status(proto.StateRunningTool, "preparing the "+spec.Repo+" workspace")
		remote := ""
		if s.cfg.GitRemote != nil {
			remote = s.cfg.GitRemote(s.open.SessionID)
		}
		p, err := tools.PrepareProject(ctx, s.cfg.Workdir, *spec, remote)
		if err != nil {
			err = fmt.Errorf("could not prepare the project workspace: %w", err)
			if s.open.Mode == proto.ModeTask {
				_ = s.conn.Send(proto.TypeDone, proto.Done{Outcome: proto.OutcomeFailed, Error: err.Error()})
				return
			}
			s.sendError(err)
		} else {
			s.exec.Project = p
		}
	}

	if s.open.Mode == proto.ModeTask {
		s.runTask(ctx)
		return
	}
	s.status(proto.StateIdle, "")
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.closed:
			return
		case msg := <-s.userCh:
			if _, err := s.turn(ctx, msg); err != nil && !errors.Is(err, context.Canceled) {
				s.sendError(err)
			}
			s.status(proto.StateIdle, "")
		}
	}
}

func (s *Session) runTask(ctx context.Context) {
	if s.open.Deadline != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, *s.open.Deadline)
		defer cancel()
	}
	goal := s.open.Goal
	if goal == "" {
		goal = proto.ResumeGoal
	}
	summary, err := s.turn(ctx, proto.TextMessage(proto.RoleUser, goal))
	done := proto.Done{Outcome: proto.OutcomeSucceeded, Summary: summary}
	switch {
	case err == nil && strings.HasPrefix(strings.TrimSpace(summary), BlockedPrefix):
		// The model reports it could not do the task; do not record that as success.
		done = proto.Done{Outcome: proto.OutcomeFailed, Summary: summary, Error: "the agent reported it was blocked"}
	case err == nil:
	case errors.Is(err, context.DeadlineExceeded):
		done = proto.Done{Outcome: proto.OutcomeTimedOut, Summary: summary, Error: "deadline exceeded"}
	case errors.Is(err, context.Canceled), errors.Is(err, errInterrupted):
		select {
		case <-s.closed:
			return // stream gone: the control plane decides what happens to the task
		default:
		}
		done = proto.Done{Outcome: proto.OutcomeCancelled, Summary: summary, Error: "interrupted"}
	default:
		done = proto.Done{Outcome: proto.OutcomeFailed, Summary: summary, Error: err.Error()}
	}
	_ = s.conn.Send(proto.TypeDone, done)
}

// read dispatches control-plane frames. Closing the stream cancels the session.
func (s *Session) read(cancel context.CancelFunc) {
	defer close(s.closed)
	defer cancel()
	for {
		env, err := s.conn.Recv()
		if err != nil {
			return
		}
		switch env.Type {
		case proto.TypeUserMessage:
			var um proto.UserMessage
			if env.Decode(&um) != nil {
				continue
			}
			um.Images = validImages(um.Images)
			if strings.TrimSpace(um.Text) != "" || len(um.Images) > 0 {
				select {
				case s.userCh <- um.Message():
				default:
					s.sendError(errors.New("message dropped: too many queued messages"))
				}
			}
		case proto.TypeInterrupt:
			s.mu.Lock()
			if s.cancelRun != nil {
				s.cancelRun()
			}
			s.mu.Unlock()
		case proto.TypeToolDecision:
			var d proto.ToolDecision
			if env.Decode(&d) != nil {
				continue
			}
			s.mu.Lock()
			ch := s.pending[d.RequestID]
			s.mu.Unlock()
			if ch != nil {
				select {
				case ch <- d:
				default:
				}
			}
		}
	}
}

// validImages keeps the references the control plane may resolve: accepted types, within the
// per-message limit. The bytes never pass through the agent.
func validImages(in []proto.ImageSource) []proto.ImageSource {
	var out []proto.ImageSource
	for _, img := range in {
		if img.AttachmentID != "" && proto.IsImageType(img.MediaType) && len(out) < proto.MaxImagesPerMessage {
			out = append(out, proto.ImageSource{AttachmentID: img.AttachmentID, MediaType: img.MediaType})
		}
	}
	return out
}

// BlockedPrefix starts a task's final message when the agent could not complete it (the system prompt
// asks for this).
const BlockedPrefix = "BLOCKED:"

var errInterrupted = errors.New("interrupted by operator")

// maxCutOffTurns caps turns whose tool input was cut off at the output limit. They do not count
// toward MaxTurns, so a model writing a large file in parts is not charged for its retries.
const maxCutOffTurns = 10

// ErrTurnLimit is returned when the model is still working after MaxTurns turns.
var ErrTurnLimit = errors.New("turn limit reached")

// turn runs the model/tool loop for one user message and returns the final assistant text.
func (s *Session) turn(parent context.Context, user proto.Message) (string, error) {
	ctx, cancel := context.WithCancel(parent)
	s.mu.Lock()
	s.cancelRun = cancel
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.cancelRun = nil
		s.mu.Unlock()
		cancel()
	}()

	if err := s.appendMessage(user); err != nil {
		return "", err
	}
	var last string
	cutOff := 0
	for turns := 0; turns < s.open.MaxTurns; turns++ {
		s.status(proto.StateThinking, "")
		ev, err := s.llm.Complete(ctx, proto.LLMRequest{SessionID: s.open.SessionID, System: s.open.System, Messages: s.history, Tools: s.defs},
			func(kind, d string) { _ = s.conn.Send(proto.TypeDelta, proto.Delta{Text: d, Kind: kind}) })
		if err != nil {
			if ctx.Err() != nil && parent.Err() == nil {
				return last, errInterrupted
			}
			return last, err
		}
		msg := *ev.Message
		msg.Role = proto.RoleAssistant
		if len(msg.Content) == 0 {
			// An empty assistant turn cannot be replayed; record why the turn ended instead.
			msg.Content = []proto.Block{{Type: proto.BlockText, Text: "(no response: " + ev.StopReason + ")"}}
		}
		if err := s.appendMessage(msg); err != nil {
			return last, err
		}
		if t := strings.TrimSpace(msg.Text()); t != "" {
			last = t
		}
		uses := msg.ToolUses()
		if ev.StopReason == proto.StopRefusal {
			// A refusal can cut a tool_use off mid-input: never run tools from this turn.
			s.closeToolUses(uses, "not run: the model declined this request")
			return last, errors.New("the model declined the request")
		}
		if len(uses) == 0 {
			return last, nil
		}
		if ev.StopReason == proto.StopMaxTokens {
			// Tool input may be truncated; do not run it. Let the model retry with smaller calls.
			s.closeToolUses(uses, "not run: the tool input was cut off at the output limit; retry with a smaller call (e.g. write the file in parts)")
			if cutOff++; cutOff > maxCutOffTurns {
				return last, fmt.Errorf("the model's output was cut off at the output limit %d times", cutOff)
			}
			turns--
			continue
		}
		results := s.runTools(ctx, uses)
		if err := s.appendMessage(proto.Message{Role: proto.RoleUser, Content: results}); err != nil {
			return last, err
		}
		if ctx.Err() != nil {
			if parent.Err() != nil {
				return last, parent.Err()
			}
			return last, errInterrupted
		}
	}
	return last, fmt.Errorf("%w: the agent was still working after %d model turns; continue the task to pick up where it stopped, or raise its max turns", ErrTurnLimit, s.open.MaxTurns)
}

// runTools executes tool calls in order. Every call gets a result block, so the history stays valid
// even when the operator interrupts mid-way.
func (s *Session) runTools(ctx context.Context, uses []proto.Block) []proto.Block {
	var results []proto.Block
	for _, u := range uses {
		if ctx.Err() != nil {
			results = append(results, toolResult(u.ID, "not run: interrupted by operator", true))
			continue
		}
		out, isErr := s.runTool(ctx, u)
		results = append(results, toolResult(u.ID, out, isErr))
	}
	return results
}

func (s *Session) runTool(ctx context.Context, u proto.Block) (string, bool) {
	return s.call(ctx, proto.ToolRequest{RequestID: newID(), ToolUseID: u.ID, Tool: u.Name, Input: u.Input})
}

// call authorises one tool call with the control plane and runs it. The local check against the
// signed bundle is made too; the control plane is asked (and audits) even when the local answer is
// deny, and the call runs only if both allow.
func (s *Session) call(ctx context.Context, req proto.ToolRequest) (string, bool) {
	local := proto.EvaluateAt(s.policy, s.open.Autonomy, s.cfg.Workdir, s.base(), proto.Call{Tool: req.Tool, Input: req.Input})
	if req.ChangeID != "" && local.Effect == proto.EffectApprove {
		// Inside an approved change the human has already approved this exact call.
		local.Effect = proto.EffectAllow
	}
	ch := make(chan proto.ToolDecision, 4)
	s.mu.Lock()
	s.pending[req.RequestID] = ch
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.pending, req.RequestID)
		s.mu.Unlock()
	}()
	if err := s.conn.Send(proto.TypeToolRequest, req); err != nil {
		return "not run: control plane unreachable", true
	}
	var final proto.ToolDecision
wait:
	for {
		select {
		case <-ctx.Done():
			return "not run: interrupted", true
		case d := <-ch:
			switch d.Effect {
			case proto.EffectApprove:
				s.status(proto.StateWaitingApproval, fmt.Sprintf("%s: %s", req.Tool, d.Reason))
			default:
				final = d
				break wait
			}
		}
	}
	if final.Effect != proto.EffectAllow {
		out := "denied: " + final.Reason
		s.report(req, out, true, 0)
		return out + ". Do not retry the same call; choose another approach or explain what you need.", true
	}
	if local.Effect == proto.EffectDeny {
		out := "denied by the agent's local policy: " + local.Reason
		logger.Warn("control plane allowed a call the local policy denies", "tool", req.Tool, "reason", local.Reason)
		s.report(req, out, true, 0)
		return out, true
	}
	if spec, ok := proto.LookupTool(req.Tool); ok && spec.Remote {
		// The control plane ran it (it holds the forge credentials); relay its result.
		if final.Result == nil {
			out := "error: the control plane returned no result for " + req.Tool
			s.report(req, out, true, 0)
			return out, true
		}
		s.report(req, final.Result.Output, final.Result.IsError, 0)
		return final.Result.Output, final.Result.IsError
	}
	if req.Tool == proto.ToolChangeRun {
		if final.ChangeID == "" {
			out := "error: the change was allowed without a change id"
			s.report(req, out, true, 0)
			return out, true
		}
		start := time.Now()
		out, isErr := s.runChange(ctx, final.ChangeID, req.Input)
		s.report(req, out, isErr, time.Since(start).Milliseconds())
		return out, isErr
	}
	s.status(proto.StateRunningTool, req.Tool)
	start := time.Now()
	res := s.exec.Run(ctx, req.Tool, req.Input)
	s.report(req, res.Output, res.IsError, time.Since(start).Milliseconds())
	return res.Output, res.IsError
}

func (s *Session) base() string {
	if s.exec.Project != nil {
		return s.exec.Project.Dir
	}
	return s.cfg.Workdir
}

func (s *Session) report(req proto.ToolRequest, out string, isErr bool, ms int64) {
	_ = s.conn.Send(proto.TypeToolResult, proto.ToolResult{RequestID: req.RequestID, ToolUseID: req.ToolUseID, Tool: req.Tool,
		Output: out, IsError: isErr, DurationMs: ms, ChangeID: req.ChangeID, Phase: req.Phase})
}

// closeToolUses answers tool_use blocks that will not run, keeping the history replayable.
func (s *Session) closeToolUses(uses []proto.Block, reason string) {
	if len(uses) == 0 {
		return
	}
	var results []proto.Block
	for _, u := range uses {
		results = append(results, toolResult(u.ID, reason, true))
	}
	_ = s.appendMessage(proto.Message{Role: proto.RoleUser, Content: results})
}

// repairHistory closes a dangling tool call left by a crash or disconnect, so the resumed history is
// a valid conversation. It only appends — history is never rewritten.
func (s *Session) repairHistory() {
	if n := len(s.history); n > 0 && s.history[n-1].Role == proto.RoleAssistant {
		s.closeToolUses(s.history[n-1].ToolUses(), "interrupted: the connection to the control plane was lost during this call, so it may or may not have run")
	}
}

func (s *Session) appendMessage(m proto.Message) error {
	s.history = append(s.history, m)
	return s.conn.Send(proto.TypeMessageAppend, proto.MessageAppend{Message: m})
}

func (s *Session) status(state, detail string) {
	_ = s.conn.Send(proto.TypeStatus, proto.Status{State: state, Detail: detail})
}

func (s *Session) sendError(err error) {
	_ = s.conn.Send(proto.TypeError, proto.Error{Message: err.Error()})
}

func toolResult(id, content string, isErr bool) proto.Block {
	return proto.Block{Type: proto.BlockToolResult, ToolUseID: id, Content: content, IsError: isErr}
}

func newID() string { return "req_" + strings.ToLower(rand.Text()[:16]) }

// DecodeOpen parses a session.open envelope.
func DecodeOpen(env proto.Envelope) (proto.SessionOpen, error) {
	var open proto.SessionOpen
	if env.Type != proto.TypeSessionOpen {
		return open, fmt.Errorf("expected %s, got %s", proto.TypeSessionOpen, env.Type)
	}
	err := json.Unmarshal(env.Payload, &open)
	return open, err
}
