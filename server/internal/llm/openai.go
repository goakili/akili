// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/goakili/akili/proto"
)

// OpenAICompatible calls a /chat/completions endpoint (OpenAI, Ollama, vLLM, LM Studio...). It is
// non-streaming: the whole reply is delivered as one delta.
type OpenAICompatible struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// NewOpenAICompatible builds a provider. baseURL defaults to https://api.openai.com/v1.
func NewOpenAICompatible(apiKey, baseURL string) *OpenAICompatible {
	return &OpenAICompatible{baseURL: normalizeBaseURL(baseURL), apiKey: apiKey, http: &http.Client{Timeout: 10 * time.Minute}}
}

// normalizeBaseURL adds /v1 to a bare host: OpenAI-compatible servers (Ollama, vLLM, LM Studio) serve
// the API under /v1, and "http://localhost:11434" is what people usually paste.
func normalizeBaseURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return "https://api.openai.com/v1"
	}
	if u, err := url.Parse(baseURL); err == nil && u.Host != "" && u.Path == "" {
		return baseURL + "/v1"
	}
	return baseURL
}

type oaMessage struct {
	Role       string       `json:"role"`
	Content    string       `json:"content"`
	ToolCalls  []oaToolCall `json:"tool_calls,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
	// Parts replaces Content for user messages with images.
	Parts []oaPart `json:"-"`
}

type oaPart struct {
	Type     string      `json:"type"`
	Text     string      `json:"text,omitempty"`
	ImageURL *oaImageURL `json:"image_url,omitempty"`
}

type oaImageURL struct {
	URL string `json:"url"`
}

func (m oaMessage) MarshalJSON() ([]byte, error) {
	type plain oaMessage
	if len(m.Parts) == 0 {
		return json.Marshal(plain(m))
	}
	return json.Marshal(struct {
		Role    string   `json:"role"`
		Content []oaPart `json:"content"`
	}{m.Role, m.Parts})
}

type oaToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// Stream implements Provider.
func (o *OpenAICompatible) Stream(ctx context.Context, req Request, onDelta DeltaFunc) (Result, error) {
	msgs := []oaMessage{{Role: "system", Content: req.System}}
	for _, m := range req.Messages {
		switch m.Role {
		case proto.RoleUser:
			var text strings.Builder
			var images []oaPart
			for _, b := range m.Content {
				switch b.Type {
				case proto.BlockToolResult:
					msgs = append(msgs, oaMessage{Role: "tool", ToolCallID: b.ToolUseID, Content: b.Content})
				case proto.BlockText:
					text.WriteString(b.Text)
				case proto.BlockImage:
					if b.Source != nil && b.Source.Data != "" {
						images = append(images, oaPart{Type: "image_url",
							ImageURL: &oaImageURL{URL: "data:" + b.Source.MediaType + ";base64," + b.Source.Data}})
					}
				}
			}
			switch {
			case len(images) > 0:
				if text.Len() > 0 {
					images = append(images, oaPart{Type: "text", Text: text.String()})
				}
				msgs = append(msgs, oaMessage{Role: "user", Parts: images})
			case text.Len() > 0:
				msgs = append(msgs, oaMessage{Role: "user", Content: text.String()})
			}
		case proto.RoleAssistant:
			am := oaMessage{Role: "assistant", Content: m.Text()}
			for _, b := range m.ToolUses() {
				tc := oaToolCall{ID: b.ID, Type: "function"}
				tc.Function.Name, tc.Function.Arguments = b.Name, string(b.Input)
				am.ToolCalls = append(am.ToolCalls, tc)
			}
			msgs = append(msgs, am)
		}
	}
	body := map[string]any{"model": req.Model, "messages": msgs}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if len(req.Tools) > 0 {
		var tools []map[string]any
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{"type": "function", "function": map[string]any{
				"name": t.Name, "description": t.Description, "parameters": t.InputSchema}})
		}
		body["tools"] = tools
	}
	b, _ := json.Marshal(body)
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return Result{}, err
	}
	hr.Header.Set("Content-Type", "application/json")
	if o.apiKey != "" {
		hr.Header.Set("Authorization", "Bearer "+o.apiKey)
	}
	resp, err := o.http.Do(hr)
	if err != nil {
		return Result{}, fmt.Errorf("openai-compatible: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("openai-compatible: %s: %s", resp.Status, truncate(string(raw), 500))
	}
	var out struct {
		Choices []struct {
			Message      oaMessage `json:"message"`
			FinishReason string    `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Choices) == 0 {
		return Result{}, fmt.Errorf("openai-compatible: bad response: %s", truncate(string(raw), 500))
	}
	c := out.Choices[0]
	msg := proto.Message{Role: proto.RoleAssistant}
	if c.Message.Content != "" {
		msg.Content = append(msg.Content, proto.Block{Type: proto.BlockText, Text: c.Message.Content})
		if onDelta != nil {
			onDelta(proto.DeltaText, c.Message.Content)
		}
	}
	for _, tc := range c.Message.ToolCalls {
		args := json.RawMessage(tc.Function.Arguments)
		if !json.Valid(args) {
			args = json.RawMessage(`{}`)
		}
		msg.Content = append(msg.Content, proto.Block{Type: proto.BlockToolUse, ID: tc.ID, Name: tc.Function.Name, Input: args})
	}
	stop := proto.StopEndTurn
	switch c.FinishReason {
	case "tool_calls":
		stop = proto.StopToolUse
	case "length":
		stop = proto.StopMaxTokens
	}
	if len(c.Message.ToolCalls) > 0 && stop == proto.StopEndTurn {
		stop = proto.StopToolUse
	}
	return Result{Message: msg, StopReason: stop, Usage: Usage{InputTokens: out.Usage.PromptTokens, OutputTokens: out.Usage.CompletionTokens}}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
