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
	"fmt"
	"strings"
	"time"

	"github.com/goakili/akili/proto"
)

// runChange is driven by the runtime, not the model; the control plane allows only calls matching the approved plan.
func (s *Session) runChange(ctx context.Context, changeID string, input json.RawMessage) (string, bool) {
	plan, err := proto.ParseChangePlan(input)
	if err != nil {
		return "error: invalid change plan: " + err.Error(), true
	}
	s.changeUpdate(changeID, proto.ChangeRunning, "")
	var b strings.Builder
	fmt.Fprintf(&b, "Change %q (%s)\n", plan.Title, changeID)

	failed := ""
	ran := 0
	for i, c := range plan.Steps {
		out, ok := s.changeCall(ctx, changeID, proto.PhaseStep, c)
		ran++
		fmt.Fprintf(&b, "\nstep %d/%d %s: %s\n%s", i+1, len(plan.Steps), c.Tool, okWord(ok), indent(out))
		if !ok {
			failed = fmt.Sprintf("step %d (%s) failed", i+1, c.Tool)
			break
		}
	}
	if failed == "" {
		for i, c := range plan.Verify {
			out, ok := s.changeCall(ctx, changeID, proto.PhaseVerify, c)
			if ok && c.Expect != "" && !strings.Contains(out, c.Expect) {
				ok = false
				out += fmt.Sprintf("\n(expected the output to contain %q)", c.Expect)
			}
			if ok && c.Reject != "" && strings.Contains(out, c.Reject) {
				ok = false
				out += fmt.Sprintf("\n(expected the output not to contain %q)", c.Reject)
			}
			fmt.Fprintf(&b, "\nverify %d/%d %s: %s\n%s", i+1, len(plan.Verify), c.Tool, okWord(ok), indent(out))
			if !ok && failed == "" {
				failed = fmt.Sprintf("verify check %d (%s) failed", i+1, c.Tool)
			}
		}
	}
	if failed == "" {
		b.WriteString("\nResult: SUCCEEDED. The change is in place and every check passed.\n")
		s.changeUpdate(changeID, proto.ChangeSucceeded, "")
		return b.String(), false
	}

	status, detail := proto.ChangeFailed, failed
	switch {
	case ran == 0:
		detail += "; nothing was changed"
	case len(plan.Rollback) == 0:
		detail += "; the plan has no rollback"
	default:
		// Roll back even if the operator interrupted: a half-applied change is the worst state.
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Minute)
		allOK := true
		for i, c := range plan.Rollback {
			out, ok := s.changeCall(rctx, changeID, proto.PhaseRollback, c)
			allOK = allOK && ok
			fmt.Fprintf(&b, "\nrollback %d/%d %s: %s\n%s", i+1, len(plan.Rollback), c.Tool, okWord(ok), indent(out))
		}
		cancel()
		if allOK {
			status, detail = proto.ChangeRolledBack, failed+"; rolled back"
		} else {
			detail += "; ROLLBACK INCOMPLETE — needs a human"
		}
	}
	fmt.Fprintf(&b, "\nResult: %s. %s.\n", strings.ToUpper(strings.ReplaceAll(status, "_", " ")), detail)
	s.changeUpdate(changeID, status, detail)
	return b.String(), true
}

func (s *Session) changeCall(ctx context.Context, changeID, phase string, c proto.ChangeCall) (string, bool) {
	out, isErr := s.call(ctx, proto.ToolRequest{RequestID: newID(), Tool: c.Tool, Input: c.Input, ChangeID: changeID, Phase: phase})
	return out, !isErr
}

func (s *Session) changeUpdate(id, status, detail string) {
	_ = s.conn.Send(proto.TypeChangeUpdate, proto.ChangeUpdate{ChangeID: id, Status: status, Detail: detail})
}

func okWord(ok bool) string {
	if ok {
		return "ok"
	}
	return "FAILED"
}

func indent(s string) string {
	s = strings.TrimRight(s, "\n")
	if len(s) > 2000 {
		s = s[:2000] + "…"
	}
	if s == "" {
		return ""
	}
	return "    " + strings.ReplaceAll(s, "\n", "\n    ") + "\n"
}
