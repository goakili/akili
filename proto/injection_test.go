// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: Apache-2.0

package proto

import (
	"encoding/json"
	"os"
	"testing"
)

type injectionCase struct {
	Name      string          `json:"name"`
	Injection string          `json:"injection"`
	Tool      string          `json:"tool"`
	Input     json.RawMessage `json:"input"`
	Expect    string          `json:"expect"`
}

func loadCorpus(t *testing.T) []injectionCase {
	t.Helper()
	b, err := os.ReadFile("testdata/injection/corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Cases []injectionCase `json:"cases"`
	}
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Cases) < 25 {
		t.Fatalf("corpus has only %d cases", len(c.Cases))
	}
	return c.Cases
}

// TestInjectionCorpus: whatever the model was talked into, the policy engine (which runs outside the
// model) never lets an injected call through beyond what the operator configured: "deny" cases are
// refused by every built-in template at every autonomy, and no call ever runs automatically above
// the autonomy level's risk ceiling, nor a critical one at all.
func TestInjectionCorpus(t *testing.T) {
	levels := []Autonomy{AutonomyL0, AutonomyL1, AutonomyL2, AutonomyL3}
	for _, c := range loadCorpus(t) {
		for _, p := range PolicyTemplates() {
			for _, a := range levels {
				d := Evaluate(p, a, wd, Call{Tool: c.Tool, Input: c.Input})
				if c.Expect == "deny" && d.Effect != EffectDeny {
					t.Errorf("%q under %s at L%d: %s (%s), want deny", c.Name, p.Name, a, d.Effect, d.Reason)
				}
				if d.Effect == EffectAllow && (d.Risk > a.autoMax() || d.Risk >= RiskCritical) {
					t.Errorf("%q under %s at L%d: %s risk ran without approval", c.Name, p.Name, a, d.Risk)
				}
				if c.Expect == "guarded" && d.Effect == EffectAllow && a < AutonomyL3 && d.Risk >= RiskHigh {
					t.Errorf("%q under %s at L%d: high-risk call auto-allowed below L3", c.Name, p.Name, a)
				}
			}
		}
	}
}

// Nested plans are refused by the plan parser itself, before any policy.
func TestInjectedNestedPlanRejected(t *testing.T) {
	for _, c := range loadCorpus(t) {
		if c.Name == "nested change plan" {
			if _, err := ParseChangePlan(c.Input); err == nil {
				t.Fatal("nested change_run accepted")
			}
		}
	}
}
