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

package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/goakili/akili/proto"
)

// llmRetryDelays are each shorter than the task lease, and the status frame sent before each wait renews it.
var llmRetryDelays = []time.Duration{2 * time.Second, 5 * time.Second, 15 * time.Second, 30 * time.Second,
	60 * time.Second, 60 * time.Second, 60 * time.Second, 60 * time.Second}

// complete retries transient failures so a long run does not die on a single provider hiccup.
func (s *Session) complete(ctx context.Context) (*proto.LLMEvent, error) {
	onDelta := func(kind, d string) { _ = s.conn.Send(proto.TypeDelta, proto.Delta{Text: d, Kind: kind}) }
	retries := 0
	for {
		ev, err := s.llm.Complete(ctx, proto.LLMRequest{SessionID: s.open.SessionID, System: s.open.System, Messages: s.view(), Tools: s.defs}, onDelta)
		if err == nil || ctx.Err() != nil {
			return ev, err
		}
		var final *FinalError
		if errors.As(err, &final) {
			if final.Code == proto.CodeContextTooLong && s.shrinkContext() {
				s.status(proto.StateThinking, "the conversation outgrew the model's context; dropping older tool output")
				continue
			}
			return nil, err
		}
		if retries >= len(llmRetryDelays) {
			return nil, fmt.Errorf("%w (gave up after %d retries)", err, retries)
		}
		wait := llmRetryDelays[retries]
		retries++
		s.status(proto.StateThinking, fmt.Sprintf("model call failed, retrying in %s: %v", wait, err))
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil, err
		case <-t.C:
		}
	}
}

// The control plane sizes the history bound from the provider's window and still records everything.
const (
	// defaultContextChars applies when the control plane sends no bound.
	defaultContextChars = 450_000
	// charsPerToken is conservative: code and JSON tokenize denser than prose.
	charsPerToken   = 3
	minContextChars = 60_000
	// keepRecent: the provider needs the last assistant turn's thinking blocks verbatim.
	keepRecent = 8
	// Blocks smaller than elideOver are kept even in trimmed messages.
	elideOver  = 512
	imageChars = 6_000
)

const elidedResult = "[output removed to save context; run the tool again if you need it]"

var elidedInput = json.RawMessage(`{"_omitted":"input removed to save context"}`)

// view only moves the trimmed prefix when the bound is crossed again, so the provider's prompt cache stays valid.
func (s *Session) view() []proto.Message {
	limit := s.contextLimit()
	s.trimmed = min(s.trimmed, len(s.history))
	size := 0
	for i, m := range s.history {
		if i < s.trimmed {
			size += msgChars(elide(m))
		} else {
			size += msgChars(m)
		}
	}
	if size > limit {
		for s.trimmed < len(s.history)-keepRecent && size > limit*3/5 {
			m := s.history[s.trimmed]
			size += msgChars(elide(m)) - msgChars(m)
			s.trimmed++
		}
	}
	if s.trimmed == 0 {
		return s.history
	}
	out := make([]proto.Message, len(s.history))
	for i, m := range s.history {
		if i < s.trimmed {
			m = elide(m)
		}
		out[i] = m
	}
	return out
}

// shrinkContext halves the context bound after the provider refused the history as too long.
func (s *Session) shrinkContext() bool {
	limit := s.contextLimit()
	if limit <= minContextChars {
		return false
	}
	s.contextChars = max(limit/2, minContextChars)
	return true
}

func (s *Session) contextLimit() int {
	switch {
	case s.contextChars > 0:
		return s.contextChars
	case s.open.ContextTokens > 0:
		return s.open.ContextTokens * charsPerToken
	}
	return defaultContextChars
}

func elide(m proto.Message) proto.Message {
	out := proto.Message{Role: m.Role, Content: make([]proto.Block, 0, len(m.Content))}
	for _, b := range m.Content {
		switch b.Type {
		case proto.BlockThinking, proto.BlockRedactedThinking:
			continue
		case proto.BlockToolResult:
			if len(b.Content) > elideOver {
				b.Content = elidedResult
			}
		case proto.BlockToolUse:
			if len(b.Input) > elideOver {
				b.Input = elidedInput
			}
		case proto.BlockImage:
			b = proto.Block{Type: proto.BlockText, Text: "[image removed to save context]"}
		}
		out.Content = append(out.Content, b)
	}
	if len(out.Content) == 0 {
		out.Content = []proto.Block{{Type: proto.BlockText, Text: "(earlier reasoning omitted)"}}
	}
	return out
}

func msgChars(m proto.Message) int {
	n := 0
	for _, b := range m.Content {
		n += len(b.Text) + len(b.Content) + len(b.Input) + len(b.Thinking) + len(b.Data)
		if b.Type == proto.BlockImage {
			n += imageChars
		}
	}
	return n
}
