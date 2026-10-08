// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/goakili/akili/proto"
)

func init() { llmRetryDelays = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond} }

// flaky fails with errs in order, then answers from the script.
type flaky struct {
	errs []error
	scripted
}

func (f *flaky) Complete(ctx context.Context, req proto.LLMRequest, d func(kind, text string)) (*proto.LLMEvent, error) {
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		f.seen = append(f.seen, req.Messages)
		return nil, err
	}
	return f.scripted.Complete(ctx, req, d)
}

func allowAll(proto.ToolRequest) []proto.ToolDecision {
	return []proto.ToolDecision{{Effect: proto.EffectAllow}}
}

func TestTransientModelErrorsAreRetried(t *testing.T) {
	llm := &flaky{errs: []error{errors.New("overloaded"), errors.New("stream reset")}, scripted: scripted{turns: []proto.LLMEvent{textTurn("done")}}}
	s := setup(t, devPolicy, llm, newFakeConn(nil), proto.ModeTask)
	if d := runTask(t, s); d.Outcome != proto.OutcomeSucceeded || d.Summary != "done" {
		t.Fatalf("done = %+v", d)
	}
}

func TestRetriesAreBounded(t *testing.T) {
	errs := make([]error, len(llmRetryDelays)+1)
	for i := range errs {
		errs[i] = errors.New("overloaded")
	}
	llm := &flaky{errs: errs, scripted: scripted{turns: []proto.LLMEvent{textTurn("never reached")}}}
	s := setup(t, devPolicy, llm, newFakeConn(nil), proto.ModeTask)
	if d := runTask(t, s); d.Outcome != proto.OutcomeFailed || !strings.Contains(d.Error, "gave up") {
		t.Fatalf("done = %+v", d)
	}
}

func TestFinalErrorsAreNotRetried(t *testing.T) {
	llm := &flaky{errs: []error{&FinalError{Code: proto.CodeBudgetExhausted, Message: "budget exhausted"}}, scripted: scripted{turns: []proto.LLMEvent{textTurn("never reached")}}}
	s := setup(t, devPolicy, llm, newFakeConn(nil), proto.ModeTask)
	if d := runTask(t, s); d.Outcome != proto.OutcomeFailed || d.Error != "budget exhausted" {
		t.Fatalf("done = %+v", d)
	}
}

func TestZeroMaxTurnsMeansNoLimit(t *testing.T) {
	read := toolTurn(proto.ToolFSRead, proto.FSReadInput{Path: "x"})
	turns := make([]proto.LLMEvent, 120)
	for i := range turns {
		turns[i] = read
	}
	s := setup(t, devPolicy, &scripted{turns: append(turns, textTurn("done"))}, newFakeConn(allowAll), proto.ModeTask)
	s.open.MaxTurns = 0
	if d := runTask(t, s); d.Outcome != proto.OutcomeSucceeded || d.Summary != "done" {
		t.Fatalf("done = %+v", d)
	}
}

func bigHistory(n int) []proto.Message {
	var h []proto.Message
	for i := range n {
		h = append(h,
			proto.Message{Role: proto.RoleAssistant, Content: []proto.Block{{Type: proto.BlockThinking, Thinking: "hmm", Signature: "sig"},
				{Type: proto.BlockToolUse, ID: "tu_" + string(rune('a'+i%26)), Name: proto.ToolFSRead, Input: []byte(`{"path":"x"}`)}}},
			proto.Message{Role: proto.RoleUser, Content: []proto.Block{toolResult("tu_"+string(rune('a'+i%26)), strings.Repeat("x", 10_000), false)}})
	}
	return h
}

func TestViewTrimsOldToolOutputAndKeepsRecent(t *testing.T) {
	s := &Session{history: bigHistory(100), contextChars: 200_000}
	v := s.view()
	if len(v) != len(s.history) {
		t.Fatalf("view has %d messages, want %d", len(v), len(s.history))
	}
	total := 0
	for _, m := range v {
		total += msgChars(m)
	}
	if total > 200_000 {
		t.Fatalf("view is %d chars, over the bound", total)
	}
	if v[1].Content[0].Content != elidedResult || len(v[0].Content) != 1 {
		t.Fatalf("oldest messages not trimmed: %+v %+v", v[0], v[1])
	}
	for _, m := range v[len(v)-keepRecent:] {
		for _, b := range m.Content {
			if b.Content == elidedResult {
				t.Fatal("a recent message was trimmed")
			}
		}
	}
	if s.history[1].Content[0].Content == elidedResult {
		t.Fatal("the recorded history was modified")
	}
	// The trimmed prefix stays put until the bound is crossed again, so the prompt cache holds.
	before := s.trimmed
	s.history = append(s.history, bigHistory(1)...)
	s.view()
	if s.trimmed != before {
		t.Fatalf("trimmed moved from %d to %d on a small append", before, s.trimmed)
	}
}

func TestContextTooLongShrinksAndRetries(t *testing.T) {
	llm := &flaky{errs: []error{&FinalError{Code: proto.CodeContextTooLong, Message: "prompt is too long"}},
		scripted: scripted{turns: []proto.LLMEvent{textTurn("done")}}}
	s := setup(t, devPolicy, llm, newFakeConn(nil), proto.ModeTask)
	s.history = bigHistory(60)
	if d := runTask(t, s); d.Outcome != proto.OutcomeSucceeded {
		t.Fatalf("done = %+v", d)
	}
	if s.contextChars != defaultContextChars/2 || s.trimmed == 0 {
		t.Fatalf("contextChars = %d, trimmed = %d", s.contextChars, s.trimmed)
	}
}

func TestContextBoundComesFromTheControlPlane(t *testing.T) {
	s := &Session{history: bigHistory(100)}
	s.open.ContextTokens = 1_000_000
	if s.view(); s.trimmed != 0 {
		t.Fatalf("a 1M-token window trimmed %d messages of a 1M-char history", s.trimmed)
	}
	s.open.ContextTokens = 50_000
	if s.view(); s.trimmed == 0 {
		t.Fatal("a 50k-token window did not trim a 1M-char history")
	}
}
