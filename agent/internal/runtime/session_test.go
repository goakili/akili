// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package runtime

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/goakili/akili/proto"
)

// fakeConn plays the control plane: it records what the agent sends and answers tool requests with
// a scripted decision.
type fakeConn struct {
	mu     sync.Mutex
	sent   []proto.Envelope
	in     chan proto.Envelope
	decide func(proto.ToolRequest) []proto.ToolDecision
}

func newFakeConn(decide func(proto.ToolRequest) []proto.ToolDecision) *fakeConn {
	return &fakeConn{in: make(chan proto.Envelope, 32), decide: decide}
}

func (f *fakeConn) Send(typ string, payload any) error {
	env, err := proto.NewEnvelope(typ, payload)
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.sent = append(f.sent, env)
	f.mu.Unlock()
	if typ == proto.TypeToolRequest {
		var req proto.ToolRequest
		_ = env.Decode(&req)
		for _, d := range f.decide(req) {
			d.RequestID = req.RequestID
			e, _ := proto.NewEnvelope(proto.TypeToolDecision, d)
			f.in <- e
		}
	}
	return nil
}

func (f *fakeConn) Recv() (proto.Envelope, error) {
	env, ok := <-f.in
	if !ok {
		return env, errors.New("closed")
	}
	return env, nil
}

func (f *fakeConn) Close() error { return nil }

func (f *fakeConn) of(typ string) []proto.Envelope {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []proto.Envelope
	for _, e := range f.sent {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

// scripted returns canned model turns in order.
type scripted struct {
	turns []proto.LLMEvent
	i     int
	seen  [][]proto.Message
}

func (s *scripted) Complete(_ context.Context, req proto.LLMRequest, _ func(kind, text string)) (*proto.LLMEvent, error) {
	s.seen = append(s.seen, append([]proto.Message(nil), req.Messages...))
	if s.i >= len(s.turns) {
		return nil, errors.New("script exhausted")
	}
	ev := s.turns[s.i]
	s.i++
	return &ev, nil
}

func toolTurn(tool string, input any) proto.LLMEvent {
	b, _ := json.Marshal(input)
	return proto.LLMEvent{Type: proto.LLMEventMessage, StopReason: proto.StopToolUse, Message: &proto.Message{Role: proto.RoleAssistant,
		Content: []proto.Block{{Type: proto.BlockToolUse, ID: "tu_1", Name: tool, Input: b}}}}
}

func textTurn(text string) proto.LLMEvent {
	m := proto.TextMessage(proto.RoleAssistant, text)
	return proto.LLMEvent{Type: proto.LLMEventMessage, StopReason: proto.StopEndTurn, Message: &m}
}

func setup(t *testing.T, pol proto.Policy, llm Completer, conn *fakeConn, mode string) *Session {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	signed, err := proto.SignPolicy(pol, priv)
	if err != nil {
		t.Fatal(err)
	}
	wd, _ := filepath.EvalSymlinks(t.TempDir())
	s, err := NewSession(Config{Workdir: wd, CPSigningKey: pub, Facts: func() proto.HostFacts { return proto.HostFacts{} }}, conn, llm,
		proto.SessionOpen{SessionID: "ses_1", Mode: mode, Goal: "do it", Policy: signed, Autonomy: proto.AutonomyL3, MaxTurns: 5,
			ToolNames: []string{proto.ToolFSWrite, proto.ToolFSRead}})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

var devPolicy = proto.Policy{Name: "dev", Tools: proto.Rule{Allow: []string{"*"}}, Paths: proto.Rule{Allow: []string{"$WORKDIR/**"}}, MaxRisk: proto.RiskHigh}

func runTask(t *testing.T, s *Session) proto.Done {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.Run(ctx)
	dones := s.conn.(*fakeConn).of(proto.TypeDone)
	if len(dones) != 1 {
		t.Fatalf("want one done frame, got %d", len(dones))
	}
	var d proto.Done
	_ = dones[0].Decode(&d)
	return d
}

func TestTaskRunsAllowedTool(t *testing.T) {
	conn := newFakeConn(func(proto.ToolRequest) []proto.ToolDecision { return []proto.ToolDecision{{Effect: proto.EffectAllow}} })
	llm := &scripted{turns: []proto.LLMEvent{toolTurn(proto.ToolFSWrite, proto.FSWriteInput{Path: "out.txt", Content: "hi"}), textTurn("wrote it")}}
	s := setup(t, devPolicy, llm, conn, proto.ModeTask)
	d := runTask(t, s)
	if d.Outcome != proto.OutcomeSucceeded || d.Summary != "wrote it" {
		t.Fatalf("done = %+v", d)
	}
	// History sent to the model on turn 2 must end with the tool result for tu_1.
	last := llm.seen[1][len(llm.seen[1])-1]
	if last.Role != proto.RoleUser || last.Content[0].ToolUseID != "tu_1" || last.Content[0].IsError {
		t.Fatalf("bad tool result in history: %+v", last)
	}
}

// TestControlPlaneDenialWins: the local policy allows, the control plane denies → the tool must not run.
func TestControlPlaneDenialWins(t *testing.T) {
	conn := newFakeConn(func(proto.ToolRequest) []proto.ToolDecision {
		return []proto.ToolDecision{{Effect: proto.EffectDeny, Reason: "nope"}}
	})
	llm := &scripted{turns: []proto.LLMEvent{toolTurn(proto.ToolFSWrite, proto.FSWriteInput{Path: "out.txt", Content: "hi"}), textTurn("ok")}}
	s := setup(t, devPolicy, llm, conn, proto.ModeTask)
	runTask(t, s)
	res := llm.seen[1][len(llm.seen[1])-1].Content[0]
	if !res.IsError {
		t.Fatalf("denied call reported as success: %+v", res)
	}
	if _, err := readFile(s.cfg.Workdir, "out.txt"); err == nil {
		t.Fatal("denied tool ran")
	}
}

// TestLocalDenialWins: a compromised or buggy control plane allows a call the signed policy denies.
func TestLocalDenialWins(t *testing.T) {
	conn := newFakeConn(func(proto.ToolRequest) []proto.ToolDecision { return []proto.ToolDecision{{Effect: proto.EffectAllow}} })
	llm := &scripted{turns: []proto.LLMEvent{toolTurn(proto.ToolFSWrite, proto.FSWriteInput{Path: "/etc/evil", Content: "x"}), textTurn("ok")}}
	s := setup(t, devPolicy, llm, conn, proto.ModeTask)
	runTask(t, s)
	if res := llm.seen[1][len(llm.seen[1])-1].Content[0]; !res.IsError {
		t.Fatalf("locally denied call ran: %+v", res)
	}
}

func TestApprovalFlow(t *testing.T) {
	conn := newFakeConn(func(proto.ToolRequest) []proto.ToolDecision {
		return []proto.ToolDecision{{Effect: proto.EffectApprove, Reason: "needs a human"}, {Effect: proto.EffectAllow, Reason: "approved"}}
	})
	llm := &scripted{turns: []proto.LLMEvent{toolTurn(proto.ToolFSWrite, proto.FSWriteInput{Path: "a.txt", Content: "x"}), textTurn("done")}}
	s := setup(t, devPolicy, llm, conn, proto.ModeTask)
	if d := runTask(t, s); d.Outcome != proto.OutcomeSucceeded {
		t.Fatalf("done = %+v", d)
	}
	var sawWaiting bool
	for _, e := range conn.of(proto.TypeStatus) {
		var st proto.Status
		_ = e.Decode(&st)
		sawWaiting = sawWaiting || st.State == proto.StateWaitingApproval
	}
	if !sawWaiting {
		t.Fatal("agent never reported waiting_approval")
	}
}

func TestMaxTokensToolIsNotRun(t *testing.T) {
	conn := newFakeConn(func(proto.ToolRequest) []proto.ToolDecision { return []proto.ToolDecision{{Effect: proto.EffectAllow}} })
	cut := toolTurn(proto.ToolFSWrite, proto.FSWriteInput{Path: "big.txt", Content: "partial"})
	cut.StopReason = proto.StopMaxTokens
	llm := &scripted{turns: []proto.LLMEvent{cut, textTurn("gave up")}}
	s := setup(t, devPolicy, llm, conn, proto.ModeTask)
	runTask(t, s)
	if len(conn.of(proto.TypeToolRequest)) != 0 {
		t.Fatal("a tool from a max_tokens turn was requested")
	}
}

// TestCutOffTurnsDoNotUseTheBudget: setup allows 5 turns; six cut-off retries must not exhaust them.
func TestCutOffTurnsDoNotUseTheBudget(t *testing.T) {
	conn := newFakeConn(func(proto.ToolRequest) []proto.ToolDecision { return []proto.ToolDecision{{Effect: proto.EffectAllow}} })
	cut := toolTurn(proto.ToolFSWrite, proto.FSWriteInput{Path: "big.txt", Content: "partial"})
	cut.StopReason = proto.StopMaxTokens
	llm := &scripted{turns: []proto.LLMEvent{cut, cut, cut, cut, cut, cut, textTurn("done")}}
	s := setup(t, devPolicy, llm, conn, proto.ModeTask)
	if d := runTask(t, s); d.Outcome != proto.OutcomeSucceeded || d.Summary != "done" {
		t.Fatalf("done = %+v", d)
	}
}

func TestCutOffTurnsAreCapped(t *testing.T) {
	conn := newFakeConn(nil)
	cut := toolTurn(proto.ToolFSWrite, proto.FSWriteInput{Path: "big.txt", Content: "partial"})
	cut.StopReason = proto.StopMaxTokens
	turns := make([]proto.LLMEvent, maxCutOffTurns+1)
	for i := range turns {
		turns[i] = cut
	}
	s := setup(t, devPolicy, &scripted{turns: append(turns, textTurn("done"))}, conn, proto.ModeTask)
	if d := runTask(t, s); d.Outcome != proto.OutcomeFailed || !strings.Contains(d.Error, "cut off") {
		t.Fatalf("done = %+v", d)
	}
}

func TestTurnLimitFailsWithAClearError(t *testing.T) {
	conn := newFakeConn(func(proto.ToolRequest) []proto.ToolDecision { return []proto.ToolDecision{{Effect: proto.EffectAllow}} })
	read := toolTurn(proto.ToolFSRead, proto.FSReadInput{Path: "x"})
	llm := &scripted{turns: []proto.LLMEvent{read, read, read, read, read, textTurn("never reached")}}
	s := setup(t, devPolicy, llm, conn, proto.ModeTask)
	d := runTask(t, s)
	if d.Outcome != proto.OutcomeFailed || !strings.Contains(d.Error, ErrTurnLimit.Error()) || !strings.Contains(d.Error, "5 model turns") {
		t.Fatalf("done = %+v", d)
	}
}

// TestAskUserWaitsForTheAnswer: the session reports waiting_input and the person's answer, relayed by
// the control plane, is the tool result.
func TestAskUserWaitsForTheAnswer(t *testing.T) {
	conn := newFakeConn(func(proto.ToolRequest) []proto.ToolDecision {
		return []proto.ToolDecision{{Effect: proto.EffectAllow, Result: &proto.RemoteResult{Output: "The user chose: Redis"}}}
	})
	ask := toolTurn(proto.ToolAskUser, proto.AskUserInput{Question: "Which cache?", Options: []proto.AskUserOption{{Label: "Redis"}, {Label: "In memory"}}})
	llm := &scripted{turns: []proto.LLMEvent{ask, textTurn("using Redis")}}
	s := setup(t, devPolicy, llm, conn, proto.ModeTask)
	if d := runTask(t, s); d.Outcome != proto.OutcomeSucceeded {
		t.Fatalf("done = %+v", d)
	}
	if res := llm.seen[1][len(llm.seen[1])-1].Content[0]; res.IsError || res.Content != "The user chose: Redis" {
		t.Fatalf("answer not relayed: %+v", res)
	}
	var waited bool
	for _, e := range conn.of(proto.TypeStatus) {
		var st proto.Status
		_ = e.Decode(&st)
		waited = waited || (st.State == proto.StateWaitingInput && st.Detail == "Which cache?")
	}
	if !waited {
		t.Fatal("agent never reported waiting_input")
	}
}

func TestBadPolicySignatureRefused(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	signed, _ := proto.SignPolicy(devPolicy, priv)
	_, err := NewSession(Config{Workdir: t.TempDir(), CPSigningKey: otherPub}, newFakeConn(nil), &scripted{},
		proto.SessionOpen{SessionID: "s", Mode: proto.ModeChat, Policy: signed})
	if !errors.Is(err, ErrBadPolicy) {
		t.Fatalf("want ErrBadPolicy, got %v", err)
	}
}

func TestRepairDanglingToolUse(t *testing.T) {
	conn := newFakeConn(nil)
	s := setup(t, devPolicy, &scripted{}, conn, proto.ModeChat)
	s.history = []proto.Message{proto.TextMessage(proto.RoleUser, "hi"), *toolTurn(proto.ToolFSRead, proto.FSReadInput{Path: "x"}).Message}
	s.repairHistory()
	last := s.history[len(s.history)-1]
	if last.Role != proto.RoleUser || last.Content[0].ToolUseID != "tu_1" || !last.Content[0].IsError {
		t.Fatalf("dangling tool_use not closed: %+v", last)
	}
}

func readFile(wd, name string) ([]byte, error) { return os.ReadFile(filepath.Join(wd, name)) }

func TestChatImagesReachTheModelAsReferences(t *testing.T) {
	conn := newFakeConn(nil)
	llm := &scripted{turns: []proto.LLMEvent{textTurn("a cat")}}
	s := setup(t, devPolicy, llm, conn, proto.ModeChat)
	um, _ := proto.NewEnvelope(proto.TypeUserMessage, proto.UserMessage{Text: "what is this?", Images: []proto.ImageSource{
		{AttachmentID: "att_1", MediaType: "image/png", Data: "c2VjcmV0"},
		{AttachmentID: "att_2", MediaType: "image/svg+xml"},
	}})
	conn.in <- um
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go s.Run(ctx)
	for len(conn.of(proto.TypeMessageAppend)) < 2 {
		if ctx.Err() != nil {
			t.Fatal("the turn did not complete")
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(conn.in)

	user := llm.seen[0][0]
	if len(user.Content) != 2 || user.Content[0].Type != proto.BlockImage || user.Content[1].Text != "what is this?" {
		t.Fatalf("user turn %+v", user)
	}
	if src := user.Content[0].Source; src.AttachmentID != "att_1" || src.Data != "" {
		t.Fatalf("image %+v: want the png reference only, without bytes; the svg must be dropped", src)
	}
}
