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
	"fmt"
	"slices"
	"strings"
)

const ToolReviewSubmit = "review_submit"

// Review verdicts are advice to the person who decides; none of them approves anything.
const (
	VerdictLooksGood = "looks_good"
	VerdictConcerns  = "concerns"
	VerdictBlock     = "block"
)

const (
	MaxReviewSummary = 4000
	MaxReviewItems   = 20
	MaxReviewItem    = 500
)

type ReviewSubmitInput struct {
	Verdict  string   `json:"verdict"`
	Summary  string   `json:"summary"`
	Risks    []string `json:"risks,omitempty"`
	Concerns []string `json:"concerns,omitempty"`
}

// reviewTools are what a reviewer may use: reading, never changing, plus submitting its review.
var reviewTools = append(append([]string{ToolFSRead, ToolFSList, ToolSearch, ToolHostInfo, ToolGitStatus, ToolGitDiff, ToolPRStatus, ToolCILogs,
	ToolReviewSubmit}, readHostTools...), miabiReadTools...)

// IsReviewTool reports whether a reviewer session may call the tool.
func IsReviewTool(name string) bool { return slices.Contains(reviewTools, name) }

func reviewResources(in json.RawMessage) (Resources, error) {
	v, err := decode[ReviewSubmitInput](in)
	if err != nil {
		return Resources{}, err
	}
	switch v.Verdict {
	case VerdictLooksGood, VerdictConcerns, VerdictBlock:
	default:
		return Resources{}, fmt.Errorf("verdict must be %s, %s or %s", VerdictLooksGood, VerdictConcerns, VerdictBlock)
	}
	if s := strings.TrimSpace(v.Summary); s == "" || len([]rune(s)) > MaxReviewSummary {
		return Resources{}, fmt.Errorf("a summary must be 1-%d characters", MaxReviewSummary)
	}
	for _, list := range [][]string{v.Risks, v.Concerns} {
		if len(list) > MaxReviewItems {
			return Resources{}, fmt.Errorf("at most %d risks and %d concerns", MaxReviewItems, MaxReviewItems)
		}
		for _, item := range list {
			if len([]rune(item)) > MaxReviewItem {
				return Resources{}, fmt.Errorf("each risk or concern is at most %d characters", MaxReviewItem)
			}
		}
	}
	return Resources{}, nil
}

func init() {
	catalog[ToolReviewSubmit] = ToolSpec{
		Name: ToolReviewSubmit, Risk: RiskLow, Remote: true,
		Description: "Submit your review: a verdict (looks_good, concerns or block), a short summary, and the risks and concerns you found. " +
			"Your review is advice shown to the person who decides; it never approves or blocks anything by itself.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"verdict":{"type":"string","enum":["looks_good","concerns","block"]},` +
			`"summary":{"type":"string","minLength":1,"maxLength":4000},"risks":{"type":"array","maxItems":20,"items":{"type":"string","maxLength":500}},` +
			`"concerns":{"type":"array","maxItems":20,"items":{"type":"string","maxLength":500}}},"required":["verdict","summary"],"additionalProperties":false}`),
		resources: reviewResources,
	}
}
