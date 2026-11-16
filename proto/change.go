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

package proto

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	PhaseStep     = "step"
	PhaseVerify   = "verify"
	PhaseRollback = "rollback"
)

const (
	ChangePending    = "pending"
	ChangeApproved   = "approved"
	ChangeDenied     = "denied"
	ChangeRunning    = "running"
	ChangeSucceeded  = "succeeded"
	ChangeFailed     = "failed"      // a step failed and rollback also failed or was not possible
	ChangeRolledBack = "rolled_back" // a step or check failed and the rollback succeeded
	ChangeExpired    = "expired"
)

const maxChangeCalls = 20

// ParseChangePlan rejects unknown tools, nested changes and plans over the call limit.
func ParseChangePlan(in json.RawMessage) (ChangePlan, error) {
	p, err := decode[ChangePlan](in)
	if err != nil {
		return p, err
	}
	if strings.TrimSpace(p.Title) == "" || strings.TrimSpace(p.Reason) == "" {
		return p, errors.New("a change needs a title and a reason")
	}
	if len(p.Steps) == 0 {
		return p, errors.New("a change needs at least one step")
	}
	if len(p.Verify) == 0 {
		return p, errors.New("a change needs at least one verify check, so failures can be caught and rolled back")
	}
	if n := len(p.Steps) + len(p.Verify) + len(p.Rollback); n > maxChangeCalls {
		return p, fmt.Errorf("a change can have at most %d calls in total (got %d)", maxChangeCalls, n)
	}
	for phase, calls := range map[string][]ChangeCall{PhaseStep: p.Steps, PhaseVerify: p.Verify, PhaseRollback: p.Rollback} {
		for i, c := range calls {
			spec, ok := LookupTool(c.Tool)
			switch {
			case !ok:
				return p, fmt.Errorf("%s %d: unknown tool %q", phase, i+1, c.Tool)
			case c.Tool == ToolChangeRun:
				return p, fmt.Errorf("%s %d: changes cannot be nested", phase, i+1)
			}
			_ = spec
			if _, err := spec.Resources(c.Input); err != nil {
				return p, fmt.Errorf("%s %d (%s): %v", phase, i+1, c.Tool, err)
			}
		}
	}
	for i, c := range p.Verify {
		if spec, _ := LookupTool(c.Tool); spec.Risk > RiskLow {
			return p, fmt.Errorf("verify %d: checks must be read-only (low-risk) tools; %s is %s risk", i+1, c.Tool, spec.Risk)
		}
	}
	return p, nil
}

// CallHash identifies one exact tool call (tool + canonical input), binding approvals to it.
func CallHash(tool string, input json.RawMessage) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, input); err != nil || buf.Len() == 0 {
		buf.Reset()
		buf.WriteString("{}")
	}
	sum := sha256.Sum256(append([]byte(tool+"\n"), buf.Bytes()...))
	return hex.EncodeToString(sum[:])
}
