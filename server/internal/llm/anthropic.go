// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/goakili/akili/proto"
	"github.com/jkaninda/logger"
)

// Anthropic calls the Claude Messages API through the official SDK.
type Anthropic struct {
	client anthropic.Client
	// fallbacks enables server-side refusal fallback ("default" routing). Only the first-party API
	// supports it, so it is off when a custom base URL is configured.
	fallbacks bool
}

// NewAnthropic builds a provider. baseURL is optional.
func NewAnthropic(apiKey, baseURL string) *Anthropic {
	opts := []option.RequestOption{option.WithAPIKey(apiKey), option.WithMaxRetries(2)}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	return &Anthropic{client: anthropic.NewClient(opts...), fallbacks: baseURL == ""}
}

// Stream implements Provider.
func (a *Anthropic) Stream(ctx context.Context, req Request, onDelta DeltaFunc) (Result, error) {
	res, err := a.stream(ctx, req, onDelta)
	if err != nil && isThinkingBindingError(err) {
		// The history no longer matches the conversation its thinking blocks were bound to (e.g. it
		// was rebuilt). Recover once without the reasoning blocks rather than failing the session.
		logger.Warn("thinking blocks rejected as bound to a different conversation; retrying without them")
		req.Messages = stripThinking(req.Messages)
		return a.stream(ctx, req, onDelta)
	}
	return res, err
}

func (a *Anthropic) stream(ctx context.Context, req Request, onDelta DeltaFunc) (Result, error) {
	params, err := a.params(req)
	if err != nil {
		return Result{}, err
	}
	var opts []option.RequestOption
	if a.fallbacks {
		opts = append(opts,
			option.WithHeaderAdd("anthropic-beta", "server-side-fallback-2026-07-01"),
			option.WithJSONSet("fallbacks", "default"))
	}
	stream := a.client.Messages.NewStreaming(ctx, params, opts...)
	msg := anthropic.Message{}
	for stream.Next() {
		ev := stream.Current()
		if err := msg.Accumulate(ev); err != nil {
			return Result{}, fmt.Errorf("anthropic: accumulate: %w", err)
		}
		if d, ok := ev.AsAny().(anthropic.ContentBlockDeltaEvent); ok && onDelta != nil {
			switch t := d.Delta.AsAny().(type) {
			case anthropic.TextDelta:
				onDelta(proto.DeltaText, t.Text)
			case anthropic.ThinkingDelta:
				onDelta(proto.DeltaThinking, t.Thinking)
			}
		}
	}
	if err := stream.Err(); err != nil {
		return Result{}, fmt.Errorf("anthropic: %w", err)
	}
	out := proto.Message{Role: proto.RoleAssistant}
	for _, b := range msg.Content {
		switch b.Type {
		case "text":
			out.Content = append(out.Content, proto.Block{Type: proto.BlockText, Text: b.Text})
		case "tool_use":
			out.Content = append(out.Content, proto.Block{Type: proto.BlockToolUse, ID: b.ID, Name: b.Name, Input: json.RawMessage(b.Input)})
		case "thinking":
			out.Content = append(out.Content, proto.Block{Type: proto.BlockThinking, Thinking: b.Thinking, Signature: b.Signature})
		case "redacted_thinking":
			out.Content = append(out.Content, proto.Block{Type: proto.BlockRedactedThinking, Data: b.Data})
		}
	}
	return Result{
		Message:    out,
		StopReason: string(msg.StopReason),
		Usage: Usage{
			InputTokens:      int(msg.Usage.InputTokens),
			OutputTokens:     int(msg.Usage.OutputTokens),
			CacheReadTokens:  int(msg.Usage.CacheReadInputTokens),
			CacheWriteTokens: int(msg.Usage.CacheCreationInputTokens),
		},
	}, nil
}

func (a *Anthropic) params(req Request) (anthropic.MessageNewParams, error) {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 32000
	}
	p := anthropic.MessageNewParams{
		Model:     anthropic.Model(req.Model),
		MaxTokens: int64(maxTokens),
		// The system prompt is fixed for a session's lifetime, so caching it (and the tools before it)
		// pays off on every turn of the agent loop.
		System: []anthropic.TextBlockParam{{Text: req.System, CacheControl: anthropic.NewCacheControlEphemeralParam()}},
		// Top-level cache control also places a breakpoint on the growing message history.
		CacheControl: anthropic.NewCacheControlEphemeralParam(),
	}
	if req.Effort != "" {
		p.OutputConfig = anthropic.OutputConfigParam{Effort: anthropic.OutputConfigEffort(req.Effort)}
	}
	if supportsAdaptiveThinking(req.Model) {
		// Reasoning is hidden by default ("omitted"), which leaves operators watching a silent pause.
		// Summaries stream while the model thinks; the blocks are replayed unchanged either way.
		p.Thinking = anthropic.ThinkingConfigParamUnion{OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{
			Display: anthropic.ThinkingConfigAdaptiveDisplaySummarized}}
	}
	for _, m := range req.Messages {
		var blocks []anthropic.ContentBlockParamUnion
		for _, b := range m.Content {
			switch b.Type {
			case proto.BlockText:
				if b.Text != "" {
					blocks = append(blocks, anthropic.NewTextBlock(b.Text))
				}
			case proto.BlockToolUse:
				input := b.Input
				if len(input) == 0 {
					input = json.RawMessage("{}")
				}
				blocks = append(blocks, anthropic.NewToolUseBlock(b.ID, input, b.Name))
			case proto.BlockToolResult:
				blocks = append(blocks, anthropic.NewToolResultBlock(b.ToolUseID, b.Content, b.IsError))
			case proto.BlockThinking:
				blocks = append(blocks, anthropic.NewThinkingBlock(b.Signature, b.Thinking))
			case proto.BlockRedactedThinking:
				blocks = append(blocks, anthropic.NewRedactedThinkingBlock(b.Data))
			case proto.BlockImage:
				if b.Source != nil && b.Source.Data != "" {
					blocks = append(blocks, anthropic.NewImageBlockBase64(b.Source.MediaType, b.Source.Data))
				}
			}
		}
		if len(blocks) == 0 {
			continue
		}
		switch m.Role {
		case proto.RoleUser:
			p.Messages = append(p.Messages, anthropic.NewUserMessage(blocks...))
		case proto.RoleAssistant:
			p.Messages = append(p.Messages, anthropic.NewAssistantMessage(blocks...))
		default:
			return p, fmt.Errorf("unknown role %q", m.Role)
		}
	}
	for _, t := range req.Tools {
		var schema struct {
			Properties any      `json:"properties"`
			Required   []string `json:"required"`
		}
		if err := json.Unmarshal(t.InputSchema, &schema); err != nil {
			return p, fmt.Errorf("tool %s schema: %w", t.Name, err)
		}
		tp := anthropic.ToolParam{
			Name:        t.Name,
			Description: anthropic.String(t.Description),
			InputSchema: anthropic.ToolInputSchemaParam{Properties: schema.Properties, Required: schema.Required},
			// Stream large inputs (file bodies) as they are generated. Inputs are then unvalidated by
			// the API: the policy engine decodes them strictly and the agent refuses to run a tool
			// from a turn that stopped on max_tokens or refusal.
			EagerInputStreaming: anthropic.Bool(true),
		}
		p.Tools = append(p.Tools, anthropic.ToolUnionParam{OfTool: &tp})
	}
	return p, nil
}

// supportsAdaptiveThinking reports whether a model takes {type: "adaptive"} thinking. Haiku 4.5 and
// older models use a token budget instead, so they get no thinking config.
func supportsAdaptiveThinking(model string) bool {
	if !strings.HasPrefix(model, "claude-") {
		return false
	}
	for _, old := range []string{"claude-haiku-", "claude-3", "claude-sonnet-4-5", "claude-opus-4-5", "claude-opus-4-1", "claude-opus-4-0", "claude-sonnet-4-0"} {
		if strings.HasPrefix(model, old) {
			return false
		}
	}
	return true
}

func isThinkingBindingError(err error) bool {
	var apiErr *anthropic.Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 400 {
		return false
	}
	return strings.Contains(err.Error(), "bound to a different conversation")
}

func stripThinking(msgs []proto.Message) []proto.Message {
	out := make([]proto.Message, 0, len(msgs))
	for _, m := range msgs {
		c := proto.Message{Role: m.Role}
		for _, b := range m.Content {
			if b.Type != proto.BlockThinking && b.Type != proto.BlockRedactedThinking {
				c.Content = append(c.Content, b)
			}
		}
		out = append(out, c)
	}
	return out
}
