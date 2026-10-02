// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: Apache-2.0

package proto

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type pipe struct {
	io.Reader
	io.Writer
}

func (pipe) Close() error { return nil }

func TestConnRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	c := NewConn(pipe{Reader: &buf, Writer: &buf})
	if err := c.Send(TypeUserMessage, UserMessage{Text: "hello\nworld"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Send(TypeInterrupt, nil); err != nil {
		t.Fatal(err)
	}
	env, err := c.Recv()
	if err != nil {
		t.Fatal(err)
	}
	var um UserMessage
	if err := env.Decode(&um); err != nil || env.Type != TypeUserMessage || um.Text != "hello\nworld" {
		t.Fatalf("got %+v %+v %v", env, um, err)
	}
	env, err = c.Recv()
	if err != nil || env.Type != TypeInterrupt || env.V != Version {
		t.Fatalf("got %+v %v", env, err)
	}
	if _, err := c.Recv(); err != io.EOF {
		t.Fatalf("want EOF, got %v", err)
	}
}

func TestConnRejectsNewerVersion(t *testing.T) {
	c := NewConn(pipe{Reader: strings.NewReader(`{"v":99,"type":"x"}` + "\n"), Writer: io.Discard})
	if _, err := c.Recv(); err != ErrVersion {
		t.Fatalf("want ErrVersion, got %v", err)
	}
}

func TestConnRejectsOversizedLine(t *testing.T) {
	big := strings.Repeat("a", MaxEnvelopeSize+10)
	c := NewConn(pipe{Reader: strings.NewReader(big + "\n"), Writer: io.Discard})
	if _, err := c.Recv(); err == nil {
		t.Fatal("oversized envelope accepted")
	}
}

// TestGolden pins the wire format of every message type. Run with UPDATE_GOLDEN=1 to regenerate
// after an intentional change, and bump Version if the change is breaking.
func TestGolden(t *testing.T) {
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	deadline := ts.Add(time.Hour)
	msgs := map[string]any{
		TypeHello:         Hello{AgentID: "ag_1", Name: "web-1", Labels: []string{"env=prod"}, MaxParallel: 2},
		TypeHeartbeat:     Heartbeat{Facts: HostFacts{Hostname: "web-1", OS: "linux", Arch: "amd64", CPUs: 4, AgentVersion: "0.1.0"}, ActiveSessions: []string{"s1"}},
		TypeDrain:         struct{}{},
		TypeSessionOpen:   SessionOpen{SessionID: "s1", TaskID: "t1", Mode: ModeTask, Goal: "check disk", System: "sys", Policy: SignedPolicy{Policy: json.RawMessage(`{"name":"p"}`), Signature: []byte{1, 2}}, Autonomy: AutonomyL1, MaxTurns: 20, Deadline: &deadline, History: []Message{TextMessage(RoleUser, "hi")}},
		TypeUserMessage:   UserMessage{Text: "hello", UserID: "u1", Images: []ImageSource{{AttachmentID: "att_1", MediaType: "image/png"}}},
		TypeInterrupt:     struct{}{},
		TypeToolDecision:  ToolDecision{RequestID: "r1", Effect: EffectApprove, Reason: "needs approval", ApprovalID: "ap1"},
		TypeStatus:        Status{State: StateThinking},
		TypeDelta:         Delta{Text: "Hel"},
		TypeMessageAppend: MessageAppend{Message: Message{Role: RoleAssistant, Content: []Block{{Type: BlockText, Text: "ok"}, {Type: BlockToolUse, ID: "tu1", Name: ToolShell, Input: json.RawMessage(`{"command":"df -h"}`)}}}},
		TypeToolRequest:   ToolRequest{RequestID: "r1", ToolUseID: "tu1", Tool: ToolShell, Input: json.RawMessage(`{"command":"df -h"}`)},
		TypeToolResult:    ToolResult{RequestID: "r1", ToolUseID: "tu1", Tool: ToolShell, Output: "Filesystem...", DurationMs: 12},
		TypeDone:          Done{Outcome: OutcomeSucceeded, Summary: "disk ok"},
		TypeError:         Error{Message: "boom"},
	}
	for typ, payload := range msgs {
		env, err := NewEnvelope(typ, payload)
		if err != nil {
			t.Fatal(err)
		}
		env.TS = ts
		env.ID = "id-1"
		got, _ := json.MarshalIndent(env, "", "  ")
		file := filepath.Join("testdata", "golden", typ+".json")
		if os.Getenv("UPDATE_GOLDEN") == "1" {
			_ = os.MkdirAll(filepath.Dir(file), 0o755)
			if err := os.WriteFile(file, append(got, '\n'), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("%s: %v (run with UPDATE_GOLDEN=1)", typ, err)
		}
		if strings.TrimSpace(string(want)) != string(got) {
			t.Errorf("%s: wire format changed\n got: %s\nwant: %s", typ, got, want)
		}
	}
}
