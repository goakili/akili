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
	"time"
)

const (
	// EnrollPath exchanges a one-time join token for an agent identity (plain HTTPS).
	EnrollPath  = "/api/v1/agent/enroll"
	ConnectPath = "/api/v1/agent/connect"
)

// The control plane checks the handshake signature, a ±60s clock window and nonce reuse.
const (
	HeaderAgentID   = "X-Akili-Agent-ID"
	HeaderTimestamp = "X-Akili-Timestamp"
	HeaderNonce     = "X-Akili-Nonce"
	HeaderSignature = "X-Akili-Signature"
	HeaderVersion   = "X-Akili-Agent-Version"
	HeaderFeatures  = "X-Akili-Features"
)

func HandshakeMessage(agentID, timestamp, nonce string) []byte {
	return []byte(agentID + "\n" + timestamp + "\n" + nonce)
}

// Internal paths carry no credentials: identity is the tunnel, and they are unreachable from the public listener.
const (
	InternalLLMPath = "/internal/llm/stream"
	// GitProxyPath prefixes git smart-HTTP requests: /internal/git/<session-id>/info/refs, ...
	GitProxyPath = "/internal/git/"
)

type EnrollRequest struct {
	JoinToken string    `json:"join_token"`
	PublicKey []byte    `json:"public_key"` // Ed25519, 32 bytes
	Facts     HostFacts `json:"facts"`
}

type EnrollResponse struct {
	AgentID       string `json:"agent_id"`
	Name          string `json:"name"`
	CPSigningKey  []byte `json:"cp_signing_key"` // Ed25519 public key that signs policy bundles
	ConnectPath   string `json:"connect_path"`
	EnrolledAtUTC string `json:"enrolled_at"`
}

type HostFacts struct {
	Hostname     string   `json:"hostname"`
	OS           string   `json:"os"`
	Arch         string   `json:"arch"`
	Kernel       string   `json:"kernel,omitempty"`
	CPUs         int      `json:"cpus"`
	MemTotalMB   uint64   `json:"mem_total_mb,omitempty"`
	MemAvailMB   uint64   `json:"mem_avail_mb,omitempty"`
	Load1        float64  `json:"load1,omitempty"`
	UptimeSec    uint64   `json:"uptime_sec,omitempty"`
	IPs          []string `json:"ips,omitempty"`
	AgentVersion string   `json:"agent_version"`
	Workdir      string   `json:"workdir,omitempty"`
}

// The control plane opens every stream; its first envelope names the stream's kind.
const (
	TypeHello     = "control.hello"     // CP → agent
	TypeHeartbeat = "control.heartbeat" // agent → CP
	TypeDrain     = "control.drain"     // CP → agent
)

type Hello struct {
	AgentID     string   `json:"agent_id"`
	Name        string   `json:"name"`
	Labels      []string `json:"labels"`
	MaxParallel int      `json:"max_parallel"`
	Draining    bool     `json:"draining"`
}

type Heartbeat struct {
	Facts          HostFacts `json:"facts"`
	ActiveSessions []string  `json:"active_sessions"`
	Draining       bool      `json:"draining"`
}

const HeartbeatInterval = 15 * time.Second

// ResumeGoal is added on resume instead of the goal, which is already in the history.
const ResumeGoal = "Continue the task from where you left off. Your previous run was interrupted before it finished; check the state before repeating any step."

const (
	TypeSessionOpen  = "session.open"  // CP → agent (first envelope)
	TypeUserMessage  = "user.message"  // CP → agent
	TypeInterrupt    = "interrupt"     // CP → agent: stop the current turn
	TypeToolDecision = "tool.decision" // CP → agent

	TypeStatus        = "status"          // agent → CP
	TypeDelta         = "assistant.delta" // agent → CP (not persisted)
	TypeMessageAppend = "message.append"  // agent → CP (persisted history)
	TypeToolRequest   = "tool.request"    // agent → CP: ask to run a tool
	TypeToolResult    = "tool.result"     // agent → CP
	TypeDone          = "done"            // agent → CP: task finished (task sessions)
	TypeChangeUpdate  = "change.update"   // agent → CP: a change plan's progress
	TypeError         = "error"           // either way
)

const (
	ModeChat = "chat" // waits for user messages until closed
	ModeTask = "task" // runs the goal to completion, then reports done
)

type SessionOpen struct {
	SessionID string    `json:"session_id"`
	TaskID    string    `json:"task_id,omitempty"`
	Mode      string    `json:"mode"`
	Goal      string    `json:"goal,omitempty"` // task mode: the first user message
	System    string    `json:"system"`         // control-plane assembled system prompt (incl. skills)
	History   []Message `json:"history,omitempty"`
	// Policy is enforced locally in addition to the control plane's authoritative check.
	Policy   SignedPolicy `json:"policy"`
	Autonomy Autonomy     `json:"autonomy"`
	MaxTurns int          `json:"max_turns"`
	// ContextTokens is the history budget after system prompt, tools and output; 0 means the agent default.
	ContextTokens int        `json:"context_tokens,omitempty"`
	Deadline      *time.Time `json:"deadline,omitempty"`
	ToolNames     []string   `json:"tool_names,omitempty"` // tools to offer the model (subset of the catalog)
	// MCPTools carry the control-plane-assigned risk so the agent can check them against its signed policy.
	MCPTools []DynamicTool `json:"mcp_tools,omitempty"`
	Project  *ProjectSpec  `json:"project,omitempty"`
}

// ProjectSpec repositories are reached only through the control plane's git proxy; the agent never holds forge credentials.
type ProjectSpec struct {
	ID            string `json:"id"`
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	Repo          string `json:"repo"` // owner/name, for display
	DefaultBranch string `json:"default_branch"`
	Branch        string `json:"branch"` // the session's working branch, akili/...
	// SandboxImage is the container image sandbox_exec runs in ("" = sandbox unavailable).
	SandboxImage string `json:"sandbox_image,omitempty"`
	// SandboxRoot is an organization setting; off, sandbox_exec runs unprivileged.
	SandboxRoot bool   `json:"sandbox_root,omitempty"`
	GitName     string `json:"git_name"`
	GitEmail    string `json:"git_email"`
	// Trailers ("Key: value") link every commit to its task or session.
	Trailers []string `json:"trailers,omitempty"`
}

type UserMessage struct {
	Text   string        `json:"text"`
	UserID string        `json:"user_id,omitempty"`
	Images []ImageSource `json:"images,omitempty"`
}

// Message puts images before the text, as providers recommend.
func (u UserMessage) Message() Message {
	m := Message{Role: RoleUser}
	for i := range u.Images {
		src := u.Images[i]
		src.Data = ""
		m.Content = append(m.Content, Block{Type: BlockImage, Source: &src})
	}
	if u.Text != "" {
		m.Content = append(m.Content, Block{Type: BlockText, Text: u.Text})
	}
	return m
}

const (
	StateIdle            = "idle"
	StateThinking        = "thinking"
	StateRunningTool     = "running_tool"
	StateWaitingApproval = "waiting_approval"
	StateWaitingInput    = "waiting_input"
)

type Status struct {
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"`
}

const (
	DeltaText     = "text"
	DeltaThinking = "thinking" // a summary of the model's reasoning, for display only
)

type Delta struct {
	Text string `json:"text"`
	Kind string `json:"kind,omitempty"` // DeltaText (default) or DeltaThinking
}

type MessageAppend struct {
	Message Message `json:"message"`
}

// ToolRequest blocks the call on the agent until a ToolDecision with the same RequestID arrives.
type ToolRequest struct {
	RequestID string          `json:"request_id"`
	ToolUseID string          `json:"tool_use_id"`
	Tool      string          `json:"tool"`
	Input     json.RawMessage `json:"input"`
	ChangeID  string          `json:"change_id,omitempty"`
	Phase     string          `json:"phase,omitempty"`
}

const (
	EffectAllow   = "allow"
	EffectDeny    = "deny"
	EffectApprove = "approve" // needs a human; a final allow/deny follows
)

type ToolDecision struct {
	RequestID  string `json:"request_id"`
	Effect     string `json:"effect"`
	Reason     string `json:"reason,omitempty"`
	ApprovalID string `json:"approval_id,omitempty"`
	// Result is set for remote tools: the control plane ran the call after allowing it.
	Result *RemoteResult `json:"result,omitempty"`
	// ChangeID is set when an approved change_run may start.
	ChangeID string `json:"change_id,omitempty"`
	// Deadline is reset after a wait on a person, which does not count toward the task timeout.
	Deadline *time.Time `json:"deadline,omitempty"`
}

type RemoteResult struct {
	Output  string `json:"output"`
	IsError bool   `json:"is_error"`
}

type ToolResult struct {
	ChangeID   string `json:"change_id,omitempty"`
	Phase      string `json:"phase,omitempty"`
	RequestID  string `json:"request_id"`
	ToolUseID  string `json:"tool_use_id"`
	Tool       string `json:"tool"`
	Output     string `json:"output"`
	IsError    bool   `json:"is_error"`
	DurationMs int64  `json:"duration_ms"`
	Truncated  bool   `json:"truncated,omitempty"`
}

type ChangeUpdate struct {
	ChangeID string `json:"change_id"`
	Status   string `json:"status"`
	Detail   string `json:"detail,omitempty"`
}

const (
	TypePTYOpen   = "pty.open"   // CP → agent (first envelope)
	TypePTYInput  = "pty.input"  // CP → agent
	TypePTYResize = "pty.resize" // CP → agent
	TypePTYOutput = "pty.output" // agent → CP
	TypePTYExit   = "pty.exit"   // agent → CP
)

// PTYOpen carries the signed policy so the agent can verify it allows terminals.
type PTYOpen struct {
	TerminalID string       `json:"terminal_id"`
	Cols       int          `json:"cols"`
	Rows       int          `json:"rows"`
	Policy     SignedPolicy `json:"policy"`
	UserID     string       `json:"user_id"`
}

type PTYData struct {
	Data []byte `json:"data"`
}

type PTYResize struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

type PTYExit struct {
	Code  int    `json:"code"`
	Error string `json:"error,omitempty"`
}

const (
	OutcomeSucceeded = "succeeded"
	OutcomeFailed    = "failed"
	OutcomeCancelled = "cancelled"
	OutcomeTimedOut  = "timed_out"
)

type Done struct {
	Outcome string `json:"outcome"`
	Summary string `json:"summary,omitempty"`
	Error   string `json:"error,omitempty"`
}

type Error struct {
	Message string `json:"message"`
}
