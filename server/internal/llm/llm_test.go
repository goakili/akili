// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package llm

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/goakili/akili/proto"
)

func TestAnthropicParamsReplayHistoryVerbatim(t *testing.T) {
	a := NewAnthropic("test-key", "")
	spec, _ := proto.LookupTool(proto.ToolShell)
	req := Request{
		Model: "claude-opus-5-5", System: "sys", MaxTokens: 1000, Effort: "high",
		Tools: []proto.ToolDef{spec.Def()},
		Messages: []proto.Message{
			proto.TextMessage(proto.RoleUser, "check disk"),
			{Role: proto.RoleAssistant, Content: []proto.Block{
				{Type: proto.BlockThinking, Thinking: "", Signature: "sig-abc"},
				{Type: proto.BlockRedactedThinking, Data: "opaque"},
				{Type: proto.BlockToolUse, ID: "tu_1", Name: proto.ToolShell, Input: json.RawMessage(`{"command":"df -h"}`)},
			}},
			{Role: proto.RoleUser, Content: []proto.Block{{Type: proto.BlockToolResult, ToolUseID: "tu_1", Content: "ok", IsError: false}}},
		},
	}
	p, err := a.params(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(p)
	body := string(b)
	for _, want := range []string{
		`"signature":"sig-abc"`,               // thinking blocks go back unchanged
		`"type":"redacted_thinking"`,          // including redacted ones
		`"data":"opaque"`,                     //
		`"eager_input_streaming":true`,        // tool inputs stream as generated
		`"cache_control":{"type":"ephemeral"`, // system + history are cached
		`"effort":"high"`,
		`"model":"claude-opus-5-5"`,
		`"tool_use_id":"tu_1"`,
		`"display":"summarized"`, // reasoning summaries stream to operators
	} {
		if !strings.Contains(body, want) {
			t.Errorf("request is missing %s\n%s", want, body)
		}
	}
	if strings.Contains(body, `"tool_choice"`) {
		t.Error("forced tool_choice must not be sent")
	}
}

func TestStripThinkingKeepsEverythingElse(t *testing.T) {
	in := []proto.Message{{Role: proto.RoleAssistant, Content: []proto.Block{
		{Type: proto.BlockThinking, Signature: "s"}, {Type: proto.BlockText, Text: "hi"}, {Type: proto.BlockToolUse, ID: "t"}}}}
	out := stripThinking(in)
	if len(out[0].Content) != 2 || out[0].Content[0].Type != proto.BlockText {
		t.Fatalf("got %+v", out)
	}
	if len(in[0].Content) != 3 {
		t.Fatal("stripThinking mutated its input")
	}
}

func TestCost(t *testing.T) {
	p := PriceFor("claude-opus-5-5", Price{})
	if p.Input != 4 || p.Output != 20 {
		t.Fatalf("price = %+v", p)
	}
	// 1M input + 1M output = $24; cache reads at 0.1x, writes at 1.25x.
	if c := Cost(Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000}, p); math.Abs(c-24) > 1e-9 {
		t.Fatalf("cost = %v", c)
	}
	if c := Cost(Usage{CacheReadTokens: 1_000_000, CacheWriteTokens: 1_000_000}, p); math.Abs(c-(0.4+5)) > 1e-9 {
		t.Fatalf("cache cost = %v", c)
	}
	if o := PriceFor("claude-opus-5-5", Price{Input: 1, Output: 2}); o.Input != 1 {
		t.Fatal("override ignored")
	}
}

func TestFakeProviderToolRoundTrip(t *testing.T) {
	f := &Fake{}
	spec, _ := proto.LookupTool(proto.ToolShell)
	res, err := f.Stream(context.Background(), Request{Tools: []proto.ToolDef{spec.Def()},
		Messages: []proto.Message{proto.TextMessage(proto.RoleUser, "run: uptime")}}, nil)
	if err != nil || res.StopReason != proto.StopToolUse || res.Message.ToolUses()[0].Name != proto.ToolShell {
		t.Fatalf("got %+v, %v", res, err)
	}
	// Without the tool offered, it must not call it.
	res, _ = f.Stream(context.Background(), Request{Messages: []proto.Message{proto.TextMessage(proto.RoleUser, "run: uptime")}}, nil)
	if res.StopReason != proto.StopEndTurn {
		t.Fatalf("called a tool that was not offered: %+v", res)
	}
}

func TestNoAdaptiveThinkingForHaiku(t *testing.T) {
	p, err := NewAnthropic("k", "").params(Request{Model: "claude-haiku-4-5", Messages: []proto.Message{proto.TextMessage(proto.RoleUser, "hi")}})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := json.Marshal(p); strings.Contains(string(b), `"thinking"`) {
		t.Fatalf("Haiku 4.5 does not take adaptive thinking: %s", b)
	}
}
