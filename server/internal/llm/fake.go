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

package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/goakili/akili/proto"
)

// Fake is a scripted provider for tests: each line of the latest user message can be a directive (run:, read:, tool:, ...), one tool per turn.
type Fake struct {
	// Delay between streamed words, to make streaming visible in the UI.
	Delay time.Duration
}

type directive struct {
	tool  string
	input any
}

var (
	reRun     = regexp.MustCompile(`^run:\s*(.+)$`)
	reRead    = regexp.MustCompile(`^read:\s*(\S+)`)
	reList    = regexp.MustCompile(`^list:\s*(\S+)`)
	reWrite   = regexp.MustCompile(`^write:\s*(\S+)\s*::\s*(.*)$`)
	reCommit  = regexp.MustCompile(`^commit:\s*(.+)$`)
	reSandbox = regexp.MustCompile(`^sandbox:\s*(.+)$`)
	rePR      = regexp.MustCompile(`^pr:\s*(.+)$`)
	reTool    = regexp.MustCompile(`^tool:\s*([a-z_]+)\s*(\{.*\})?\s*$`)
	reChange  = regexp.MustCompile(`^change:\s*(\{.*\})\s*$`)
)

func parseDirective(line string) (directive, bool) {
	line = strings.TrimSpace(line)
	lower := strings.ToLower(line)
	switch {
	case reRun.MatchString(line):
		return directive{proto.ToolShell, proto.ShellInput{Command: strings.TrimSpace(reRun.FindStringSubmatch(line)[1])}}, true
	case reRead.MatchString(line):
		return directive{proto.ToolFSRead, proto.FSReadInput{Path: reRead.FindStringSubmatch(line)[1]}}, true
	case reList.MatchString(line):
		return directive{proto.ToolFSList, proto.FSListInput{Path: reList.FindStringSubmatch(line)[1]}}, true
	case reWrite.MatchString(line):
		m := reWrite.FindStringSubmatch(line)
		return directive{proto.ToolFSWrite, proto.FSWriteInput{Path: m[1], Content: strings.ReplaceAll(m[2], `\n`, "\n")}}, true
	case reCommit.MatchString(line):
		return directive{proto.ToolGitCommit, proto.GitCommitInput{Message: reCommit.FindStringSubmatch(line)[1]}}, true
	case reSandbox.MatchString(line):
		return directive{proto.ToolSandboxExec, proto.SandboxExecInput{Command: reSandbox.FindStringSubmatch(line)[1]}}, true
	case rePR.MatchString(line):
		return directive{proto.ToolPROpen, proto.PROpenInput{Title: rePR.FindStringSubmatch(line)[1], Body: "Scripted change."}}, true
	case reTool.MatchString(line):
		m := reTool.FindStringSubmatch(line)
		in := json.RawMessage(m[2])
		if len(in) == 0 {
			in = json.RawMessage(`{}`)
		}
		return directive{m[1], in}, true
	case reChange.MatchString(line):
		return directive{proto.ToolChangeRun, json.RawMessage(reChange.FindStringSubmatch(line)[1])}, true
	case lower == "host" || strings.Contains(lower, "host info"):
		return directive{proto.ToolHostInfo, proto.HostInfoInput{}}, true
	case lower == "status":
		return directive{proto.ToolGitStatus, proto.EmptyInput{}}, true
	case lower == "diff":
		return directive{proto.ToolGitDiff, proto.GitDiffInput{}}, true
	case lower == "push":
		return directive{proto.ToolGitPush, proto.EmptyInput{}}, true
	case lower == "checks":
		return directive{proto.ToolPRStatus, proto.EmptyInput{}}, true
	}
	return directive{}, false
}

func (f *Fake) Stream(ctx context.Context, req Request, onDelta DeltaFunc) (Result, error) {
	if len(req.Messages) == 0 {
		return Result{}, fmt.Errorf("fake: no messages")
	}
	usage := Usage{InputTokens: 10 * len(req.Messages), OutputTokens: 20}

	// Count the tools run since the latest user text, so a resumed task continues its script.
	start := -1
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if m := req.Messages[i]; m.Role == proto.RoleUser && strings.TrimSpace(m.Text()) != "" && m.Text() != proto.ResumeGoal {
			start = i
			break
		}
	}
	if start < 0 {
		return f.text(ctx, "Nothing to do.", usage, onDelta)
	}
	text := strings.TrimSpace(req.Messages[start].Text())
	if text == "images?" {
		return f.text(ctx, describeImages(req.Messages[start]), usage, onDelta)
	}
	if needle, ok := strings.CutPrefix(text, "system-has:"); ok {
		answer := "no"
		if strings.Contains(req.System, strings.TrimSpace(needle)) {
			answer = "yes"
		}
		return f.text(ctx, "system-has: "+answer, usage, onDelta)
	}
	var script []directive
	for _, line := range strings.Split(text, "\n") {
		if d, ok := parseDirective(line); ok {
			script = append(script, d)
		}
	}
	done := 0
	var lastResult *proto.Block
	for _, m := range req.Messages[start+1:] {
		done += len(m.ToolUses())
		for i := range m.Content {
			if m.Content[i].Type == proto.BlockToolResult {
				lastResult = &m.Content[i]
			}
		}
	}
	if done < len(script) {
		d := script[done]
		if !hasTool(req.Tools, d.tool) {
			return f.text(ctx, fmt.Sprintf("BLOCKED: I need the %s tool, which is not available in this session.", d.tool), usage, onDelta)
		}
		raw, _ := json.Marshal(d.input)
		intro := fmt.Sprintf("I'll use %s.", d.tool)
		if onDelta != nil {
			onDelta(proto.DeltaThinking, "The request names a tool directly; calling it.")
			onDelta(proto.DeltaText, intro)
		}
		return Result{
			Message: proto.Message{Role: proto.RoleAssistant, Content: []proto.Block{
				{Type: proto.BlockText, Text: intro},
				{Type: proto.BlockToolUse, ID: "toolu_" + fmt.Sprint(time.Now().UnixNano()), Name: d.tool, Input: raw},
			}},
			StopReason: proto.StopToolUse,
			Usage:      usage,
		}, nil
	}
	if lastResult != nil {
		status := "succeeded"
		if lastResult.IsError {
			status = "failed"
		}
		out := lastResult.Content
		if len(out) > 800 {
			out = out[:800] + "…"
		}
		return f.text(ctx, fmt.Sprintf("The tool call %s:\n\n```\n%s\n```\n\nDone.", status, strings.TrimSpace(out)), usage, onDelta)
	}
	return f.text(ctx, "Echo: "+text, usage, onDelta)
}

func (f *Fake) text(ctx context.Context, s string, usage Usage, onDelta DeltaFunc) (Result, error) {
	for _, w := range strings.SplitAfter(s, " ") {
		if onDelta != nil {
			onDelta(proto.DeltaText, w)
		}
		if f.Delay > 0 {
			select {
			case <-ctx.Done():
				return Result{}, ctx.Err()
			case <-time.After(f.Delay):
			}
		}
	}
	return Result{Message: proto.TextMessage(proto.RoleAssistant, s), StopReason: proto.StopEndTurn, Usage: usage}, nil
}

func hasTool(tools []proto.ToolDef, name string) bool {
	for _, t := range tools {
		if t.Name == name {
			return true
		}
	}
	return false
}

// describeImages answers "images?" so tests can check that the gateway resolved the images.
func describeImages(m proto.Message) string {
	var seen []string
	for _, b := range m.Content {
		if b.Type == proto.BlockImage && b.Source != nil && b.Source.Data != "" {
			raw, _ := base64.StdEncoding.DecodeString(b.Source.Data)
			seen = append(seen, fmt.Sprintf("%s %d bytes", b.Source.MediaType, len(raw)))
		}
	}
	if len(seen) == 0 {
		return "images: none"
	}
	return "images: " + strings.Join(seen, ", ")
}
