// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: Apache-2.0

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

// Change phases.
const (
	PhaseStep     = "step"
	PhaseVerify   = "verify"
	PhaseRollback = "rollback"
)

// Change statuses.
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

// ParseChangePlan decodes and validates a plan: known tools, no nesting, no remote tools, limits.
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
