// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goakili/akili/proto"
)

func changeTurn(t *testing.T, plan string) proto.LLMEvent {
	t.Helper()
	return proto.LLMEvent{Type: proto.LLMEventMessage, StopReason: proto.StopToolUse, Message: &proto.Message{Role: proto.RoleAssistant,
		Content: []proto.Block{{Type: proto.BlockToolUse, ID: "tu_c", Name: proto.ToolChangeRun, Input: json.RawMessage(plan)}}}}
}

// allowChange plays a control plane that approves the change and allows its calls.
func allowChange(req proto.ToolRequest) []proto.ToolDecision {
	if req.Tool == proto.ToolChangeRun {
		return []proto.ToolDecision{{Effect: proto.EffectApprove}, {Effect: proto.EffectAllow, ChangeID: "chg_1"}}
	}
	if req.ChangeID != "chg_1" {
		return []proto.ToolDecision{{Effect: proto.EffectDeny, Reason: "not part of the change"}}
	}
	return []proto.ToolDecision{{Effect: proto.EffectAllow}}
}

func lastChangeStatus(t *testing.T, c *fakeConn) string {
	t.Helper()
	ups := c.of(proto.TypeChangeUpdate)
	if len(ups) == 0 {
		t.Fatal("no change updates")
	}
	var u proto.ChangeUpdate
	_ = ups[len(ups)-1].Decode(&u)
	return u.Status
}

var changePolicy = proto.Policy{Name: "ops", Tools: proto.Rule{Allow: []string{"*"}}, Paths: proto.Rule{Allow: []string{"$WORKDIR/**"}}, MaxRisk: proto.RiskHigh}

func TestChangeSucceeds(t *testing.T) {
	conn := newFakeConn(allowChange)
	plan := `{"title":"write config","reason":"test","steps":[{"tool":"fs_write","input":{"path":"app.conf","content":"v=2"}}],
		"verify":[{"tool":"fs_read","input":{"path":"app.conf"},"expect":"v=2"}],"rollback":[{"tool":"fs_write","input":{"path":"app.conf","content":"v=1"}}]}`
	s := setup(t, changePolicy, &scripted{turns: []proto.LLMEvent{changeTurn(t, plan), textTurn("done")}}, conn, proto.ModeTask)
	runTask(t, s)
	if st := lastChangeStatus(t, conn); st != proto.ChangeSucceeded {
		t.Fatalf("status = %s", st)
	}
	if b, _ := os.ReadFile(filepath.Join(s.cfg.Workdir, "app.conf")); string(b) != "v=2" {
		t.Fatalf("file = %q", b)
	}
}

func TestChangeRollsBackWhenVerifyFails(t *testing.T) {
	conn := newFakeConn(allowChange)
	plan := `{"title":"write config","reason":"test","steps":[{"tool":"fs_write","input":{"path":"app.conf","content":"broken"}}],
		"verify":[{"tool":"fs_read","input":{"path":"app.conf"},"expect":"v=2"}],"rollback":[{"tool":"fs_write","input":{"path":"app.conf","content":"v=1"}}]}`
	llm := &scripted{turns: []proto.LLMEvent{changeTurn(t, plan), textTurn("rolled back")}}
	s := setup(t, changePolicy, llm, conn, proto.ModeTask)
	runTask(t, s)
	if st := lastChangeStatus(t, conn); st != proto.ChangeRolledBack {
		t.Fatalf("status = %s", st)
	}
	if b, _ := os.ReadFile(filepath.Join(s.cfg.Workdir, "app.conf")); string(b) != "v=1" {
		t.Fatalf("rollback did not restore the file: %q", b)
	}
	res := llm.seen[1][len(llm.seen[1])-1].Content[0]
	if !res.IsError || !strings.Contains(res.Content, "ROLLED BACK") {
		t.Fatalf("model was not told about the rollback: %+v", res)
	}
}

func TestChangeStopsAtFailedStepAndSkipsVerify(t *testing.T) {
	conn := newFakeConn(allowChange)
	plan := `{"title":"x","reason":"y","steps":[{"tool":"fs_read","input":{"path":"missing.txt"}},{"tool":"fs_write","input":{"path":"never.txt","content":"x"}}],
		"verify":[{"tool":"host_info","input":{}}]}`
	s := setup(t, changePolicy, &scripted{turns: []proto.LLMEvent{changeTurn(t, plan), textTurn("failed")}}, conn, proto.ModeTask)
	runTask(t, s)
	if _, err := os.Stat(filepath.Join(s.cfg.Workdir, "never.txt")); err == nil {
		t.Fatal("a step after the failed one ran")
	}
	if st := lastChangeStatus(t, conn); st != proto.ChangeFailed {
		t.Fatalf("status = %s", st)
	}
}
