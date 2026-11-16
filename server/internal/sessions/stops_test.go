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

package sessions

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func stopHub(t *testing.T) (*Hub, *models.ChatSession) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.SessionEvent{}, &models.Question{}); err != nil {
		t.Fatal(err)
	}
	return &Hub{db: db}, &models.ChatSession{Base: models.Base{ID: "ses_1", OrganizationID: "org_1"}, Mode: proto.ModeTask}
}

var at = time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)

func call(t *testing.T, h *Hub, n int, tool, input string, isErr bool) {
	t.Helper()
	id := fmt.Sprintf("req_%d", n)
	req, _ := json.Marshal(map[string]any{"request_id": id, "tool": tool, "input": json.RawMessage(input)})
	res, _ := json.Marshal(proto.ToolResult{RequestID: id, Tool: tool, IsError: isErr})
	ts := at.Add(time.Duration(n) * time.Second)
	h.db.Create(&models.SessionEvent{SessionID: "ses_1", Type: EvToolRequest, Payload: req, CreatedAt: ts})
	h.db.Create(&models.SessionEvent{SessionID: "ses_1", Type: EvToolResult, Payload: res, CreatedAt: ts})
}

func TestStopOnRepeatedFailure(t *testing.T) {
	h, s := stopHub(t)
	req := proto.ToolRequest{Tool: proto.ToolShell, Input: json.RawMessage(`{"command":"make test"}`)}
	for i := range 2 {
		call(t, h, i, proto.ToolShell, `{"command":"make test"}`, true)
	}
	call(t, h, 2, proto.ToolShell, `{"command":"ls"}`, false)
	if r := h.stopReason(context.Background(), s, req); r != "" {
		t.Fatalf("stopped after two failures: %s", r)
	}
	call(t, h, 3, proto.ToolShell, `{"command":"make test"}`, true)
	if r := h.stopReason(context.Background(), s, req); !strings.Contains(r, "failed 3 times") {
		t.Fatalf("reason = %q", r)
	}
	// An operator's answer starts a fresh count.
	h.db.Create(&models.Question{Base: models.Base{ID: "qst_1", OrganizationID: "org_1", CreatedAt: at.Add(10 * time.Second)}, SessionID: "ses_1",
		AgentID: "ag_1", RequestID: "req_x", Question: "q", Kind: models.QuestionStop, Status: models.QuestionAnswered})
	if r := h.stopReason(context.Background(), s, req); r != "" {
		t.Fatalf("count not reset after the operator answered: %s", r)
	}
}

func TestStopOnErrorStreak(t *testing.T) {
	h, s := stopHub(t)
	for i := range 15 {
		call(t, h, i, proto.ToolShell, fmt.Sprintf(`{"command":"try %d"}`, i), true)
	}
	if r := h.stopReason(context.Background(), s, proto.ToolRequest{Tool: proto.ToolFSRead, Input: json.RawMessage(`{"path":"a"}`)}); !strings.Contains(r, "last 15") {
		t.Fatalf("reason = %q", r)
	}
}

func TestStopIgnoresAskUser(t *testing.T) {
	h, s := stopHub(t)
	for i := range 20 {
		call(t, h, i, proto.ToolAskUser, `{"question":"?"}`, true)
	}
	if r := h.stopReason(context.Background(), s, proto.ToolRequest{Tool: proto.ToolFSRead, Input: json.RawMessage(`{"path":"a"}`)}); r != "" {
		t.Fatalf("ask_user counted: %s", r)
	}
}

func TestStopDecisionNeverRunsTheHeldCall(t *testing.T) {
	zero, one := 0, 1
	for _, q := range []models.Question{
		{RequestID: "r", Choice: &zero, Answer: stopContinue},
		{RequestID: "r", Choice: &one, Answer: stopEnd},
		{RequestID: "r", Answer: "try the staging host instead"},
	} {
		d := stopDecision(&q, false)
		if d.Effect != proto.EffectDeny || d.RequestID != "r" || d.Result != nil {
			t.Fatalf("answer %q: %+v", q.Answer, d)
		}
	}
	if d := stopDecision(&models.Question{RequestID: "r"}, true); d.Effect != proto.EffectDeny || !strings.Contains(d.Reason, "BLOCKED") {
		t.Fatalf("expired: %+v", d)
	}
}
