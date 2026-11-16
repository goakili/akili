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
	"slices"
)

// Conversation types are provider-neutral: the gateway translates them, so the agent never sees a model key.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

const (
	BlockText       = "text"
	BlockToolUse    = "tool_use"
	BlockToolResult = "tool_result"
	// Reasoning blocks are replayed byte-for-byte because the provider binds them to the exact prefix.
	BlockThinking         = "thinking"
	BlockRedactedThinking = "redacted_thinking"
	// BlockImage history carries only an attachment reference; the control plane fills in the bytes.
	BlockImage = "image"
)

const (
	MaxImageBytes       = 5 << 20 // the strictest provider limit (Anthropic)
	MaxImagesPerMessage = 5
)

// ImageTypes excludes SVG because it can carry script.
var ImageTypes = []string{"image/png", "image/jpeg", "image/gif", "image/webp"}

func IsImageType(mediaType string) bool { return slices.Contains(ImageTypes, mediaType) }

type ImageSource struct {
	AttachmentID string `json:"attachment_id"`
	MediaType    string `json:"media_type"`
	// Data is base64, set only by the control plane for the provider call; never stored or sent to agents.
	Data string `json:"data,omitempty"`
}

type Message struct {
	Role    string  `json:"role"`
	Content []Block `json:"content"`
}

type Block struct {
	Type string `json:"type"`
	// text
	Text string `json:"text,omitempty"`
	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
	// thinking / redacted_thinking
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
	Data      string `json:"data,omitempty"`
	// image
	Source *ImageSource `json:"source,omitempty"`
}

func TextMessage(role, text string) Message {
	return Message{Role: role, Content: []Block{{Type: BlockText, Text: text}}}
}

func (m Message) Text() string {
	var s string
	for _, b := range m.Content {
		if b.Type == BlockText {
			s += b.Text
		}
	}
	return s
}

func (m Message) ToolUses() []Block {
	var out []Block
	for _, b := range m.Content {
		if b.Type == BlockToolUse {
			out = append(out, b)
		}
	}
	return out
}

type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type LLMRequest struct {
	SessionID string    `json:"session_id"`
	System    string    `json:"system"`
	Messages  []Message `json:"messages"`
	Tools     []ToolDef `json:"tools,omitempty"`
	MaxTokens int       `json:"max_tokens,omitempty"`
}

const (
	LLMEventDelta   = "delta"   // Text
	LLMEventMessage = "message" // Message, StopReason, Usage — the final assembled message
	LLMEventError   = "error"   // Error
)

const (
	StopEndTurn   = "end_turn"
	StopToolUse   = "tool_use"
	StopMaxTokens = "max_tokens"
	StopRefusal   = "refusal"
)

type LLMEvent struct {
	Type       string   `json:"type"`
	Text       string   `json:"text,omitempty"`
	Kind       string   `json:"kind,omitempty"` // delta kind: DeltaText or DeltaThinking
	Message    *Message `json:"message,omitempty"`
	StopReason string   `json:"stop_reason,omitempty"`
	Usage      *Usage   `json:"usage,omitempty"`
	Error      string   `json:"error,omitempty"`
	// Code is set on errors the agent should treat as final.
	Code string `json:"code,omitempty"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

const (
	CodeBudgetExhausted = "budget_exhausted"
	CodeNoProvider      = "no_provider"
	CodeSessionClosed   = "session_closed"
	// CodeProviderError is a failed model call that may succeed when retried (overload, rate limit, network).
	CodeProviderError = "provider_error"
	// CodeProviderRejected means the provider refused the request itself; resending it will not help.
	CodeProviderRejected = "provider_rejected"
	CodeContextTooLong   = "context_too_long"
)
