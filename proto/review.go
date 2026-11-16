// Copyright (c) 2026 Jonas Kaninda.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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
