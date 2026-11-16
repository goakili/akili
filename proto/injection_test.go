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

// TestInjectionCorpus: deny cases fail under every template, and nothing auto-runs above the autonomy ceiling.
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
