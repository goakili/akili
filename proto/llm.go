// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: Apache-2.0

package proto

import (
	"encoding/json"
	"slices"
)

// Provider-neutral conversation types. The agent speaks these to the control plane's LLM gateway,
// which translates them for the configured provider. The agent never chooses the model or sees a key.

// Roles.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Block types.
const (
	BlockText       = "text"
	BlockToolUse    = "tool_use"
	BlockToolResult = "tool_result"
	// Provider reasoning blocks. They are opaque: stored and replayed byte-for-byte in an
	// append-only history, because the provider binds them to the exact conversation prefix.
	BlockThinking         = "thinking"
	BlockRedactedThinking = "redacted_thinking"
	// BlockImage is an image the user attached. History carries only a reference to the stored
	// attachment; the control plane fills in the bytes when it calls the provider.
	BlockImage = "image"
)

// Image limits shared by the UI, the control plane and the agent.
const (
	MaxImageBytes       = 5 << 20 // the strictest provider limit (Anthropic)
	MaxImagesPerMessage = 5
)

// ImageTypes are the media types accepted for image attachments. SVG is excluded: it is a document
// that can carry script, not a picture.
var ImageTypes = []string{"image/png", "image/jpeg", "image/gif", "image/webp"}

// IsImageType reports whether mediaType is an accepted image type.
func IsImageType(mediaType string) bool { return slices.Contains(ImageTypes, mediaType) }

// ImageSource locates an image block's bytes.
type ImageSource struct {
	AttachmentID string `json:"attachment_id"`
	MediaType    string `json:"media_type"`
	// Data is base64, set only by the control plane for the provider call; never stored or sent to agents.
	Data string `json:"data,omitempty"`
}

// Message is one conversation turn.
type Message struct {
	Role    string  `json:"role"`
	Content []Block `json:"content"`
}

// Block is one piece of message content.
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

// TextMessage builds a single-text-block message.
func TextMessage(role, text string) Message {
	return Message{Role: role, Content: []Block{{Type: BlockText, Text: text}}}
}

// Text concatenates the text blocks of a message.
func (m Message) Text() string {
	var s string
	for _, b := range m.Content {
		if b.Type == BlockText {
			s += b.Text
		}
	}
	return s
}

// ToolUses returns the tool_use blocks of a message.
func (m Message) ToolUses() []Block {
	var out []Block
	for _, b := range m.Content {
		if b.Type == BlockToolUse {
			out = append(out, b)
		}
	}
	return out
}

// ToolDef is a tool offered to the model.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// LLMRequest is posted by the agent to InternalLLMPath.
type LLMRequest struct {
	SessionID string    `json:"session_id"`
	System    string    `json:"system"`
	Messages  []Message `json:"messages"`
	Tools     []ToolDef `json:"tools,omitempty"`
	MaxTokens int       `json:"max_tokens,omitempty"`
}

// LLM stream event types (NDJSON lines in the response body).
const (
	LLMEventDelta   = "delta"   // Text
	LLMEventMessage = "message" // Message, StopReason, Usage — the final assembled message
	LLMEventError   = "error"   // Error
)

// Stop reasons.
const (
	StopEndTurn   = "end_turn"
	StopToolUse   = "tool_use"
	StopMaxTokens = "max_tokens"
	StopRefusal   = "refusal"
)

// LLMEvent is one line of the gateway's streamed response.
type LLMEvent struct {
	Type       string   `json:"type"`
	Text       string   `json:"text,omitempty"`
	Kind       string   `json:"kind,omitempty"` // delta kind: DeltaText or DeltaThinking
	Message    *Message `json:"message,omitempty"`
	StopReason string   `json:"stop_reason,omitempty"`
	Usage      *Usage   `json:"usage,omitempty"`
	Error      string   `json:"error,omitempty"`
	// Code is set on errors the agent should treat as final (e.g. "budget_exhausted").
	Code string `json:"code,omitempty"`
}

// Usage is token accounting for one model call.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Error codes on LLMEvent.
const (
	CodeBudgetExhausted = "budget_exhausted"
	CodeNoProvider      = "no_provider"
	CodeSessionClosed   = "session_closed"
	// CodeProviderError is a failed model call that may succeed when retried (overload, rate limit, network).
	CodeProviderError = "provider_error"
	// CodeProviderRejected means the provider refused the request itself; resending it will not help.
	CodeProviderRejected = "provider_rejected"
	// CodeContextTooLong means the conversation no longer fits the model's context window.
	CodeContextTooLong = "context_too_long"
)
