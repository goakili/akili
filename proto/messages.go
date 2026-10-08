// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: Apache-2.0

package proto

import (
	"encoding/json"
	"time"
)

// Paths on the control plane.
const (
	// EnrollPath exchanges a one-time join token for an agent identity (plain HTTPS).
	EnrollPath = "/api/v1/agent/enroll"
	// ConnectPath upgrades to the agent tunnel.
	ConnectPath = "/api/v1/agent/connect"
)

// Handshake headers on ConnectPath. The agent signs "<agent-id>\n<timestamp>\n<nonce>" with its
// Ed25519 key; the control plane checks the signature, a ±60s clock window and nonce reuse.
const (
	HeaderAgentID   = "X-Akili-Agent-ID"
	HeaderTimestamp = "X-Akili-Timestamp"
	HeaderNonce     = "X-Akili-Nonce"
	HeaderSignature = "X-Akili-Signature"
	HeaderVersion   = "X-Akili-Agent-Version"
	HeaderFeatures  = "X-Akili-Features"
)

// HandshakeMessage is the byte string an agent signs on connect.
func HandshakeMessage(agentID, timestamp, nonce string) []byte {
	return []byte(agentID + "\n" + timestamp + "\n" + nonce)
}

// Internal API the control plane serves to an agent over its tunnel (agent-opened streams). Identity
// is the tunnel itself, so these carry no credentials and are unreachable from the public listener.
const (
	InternalLLMPath = "/internal/llm/stream"
	// GitProxyPath prefixes git smart-HTTP requests: /internal/git/<session-id>/info/refs, ...
	GitProxyPath = "/internal/git/"
)

// EnrollRequest is posted by `akili-agent enroll`.
type EnrollRequest struct {
	JoinToken string    `json:"join_token"`
	PublicKey []byte    `json:"public_key"` // Ed25519, 32 bytes
	Facts     HostFacts `json:"facts"`
}

// EnrollResponse binds the agent and pins the control plane's policy-signing key.
type EnrollResponse struct {
	AgentID       string `json:"agent_id"`
	Name          string `json:"name"`
	CPSigningKey  []byte `json:"cp_signing_key"` // Ed25519 public key that signs policy bundles
	ConnectPath   string `json:"connect_path"`
	EnrolledAtUTC string `json:"enrolled_at"`
}

// HostFacts describes the machine an agent runs on.
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

// ---- Stream kinds ----------------------------------------------------------------------------
//
// The control plane opens every stream; the first envelope names its kind:
//   control.hello → the control stream (one per tunnel)
//   session.open  → a chat or task session

// Control stream.
const (
	TypeHello     = "control.hello"     // CP → agent
	TypeHeartbeat = "control.heartbeat" // agent → CP
	TypeDrain     = "control.drain"     // CP → agent
)

// Hello is the first envelope on the control stream.
type Hello struct {
	AgentID     string   `json:"agent_id"`
	Name        string   `json:"name"`
	Labels      []string `json:"labels"`
	MaxParallel int      `json:"max_parallel"`
	Draining    bool     `json:"draining"`
}

// Heartbeat reports liveness and load every HeartbeatInterval.
type Heartbeat struct {
	Facts          HostFacts `json:"facts"`
	ActiveSessions []string  `json:"active_sessions"`
	Draining       bool      `json:"draining"`
}

// HeartbeatInterval is how often an agent reports.
const HeartbeatInterval = 15 * time.Second

// ResumeGoal is the user turn an agent adds when it resumes a task (after a reconnect, or when an
// operator continues a stopped task): the original goal is already in the history.
const ResumeGoal = "Continue the task from where you left off. Your previous run was interrupted before it finished; check the state before repeating any step."

// Session stream.
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

// Session modes.
const (
	ModeChat = "chat" // waits for user messages until closed
	ModeTask = "task" // runs the goal to completion, then reports done
)

// SessionOpen starts a session on the agent.
type SessionOpen struct {
	SessionID string    `json:"session_id"`
	TaskID    string    `json:"task_id,omitempty"`
	Mode      string    `json:"mode"`
	Goal      string    `json:"goal,omitempty"` // task mode: the first user message
	System    string    `json:"system"`         // control-plane assembled system prompt (incl. skills)
	History   []Message `json:"history,omitempty"`
	// Policy is the signed bundle the agent enforces locally, in addition to the control plane's
	// authoritative check of every tool request.
	Policy   SignedPolicy `json:"policy"`
	Autonomy Autonomy     `json:"autonomy"`
	MaxTurns int          `json:"max_turns"`
	// ContextTokens is how many tokens of history fit the model's context once the system prompt,
	// tools and output are reserved; 0 lets the agent use its default.
	ContextTokens int        `json:"context_tokens,omitempty"`
	Deadline      *time.Time `json:"deadline,omitempty"`
	ToolNames     []string   `json:"tool_names,omitempty"` // tools to offer the model (subset of the catalog)
	// MCPTools are the MCP tools among ToolNames, with the risk the control plane assigned, so the
	// agent can describe them to the model and check them against its signed policy.
	MCPTools []DynamicTool `json:"mcp_tools,omitempty"`
	// Project, when set, binds the session to a repository workspace on its own branch.
	Project *ProjectSpec `json:"project,omitempty"`
}

// ProjectSpec tells the agent which repository workspace to prepare. The agent reaches the
// repository only through the control plane's git proxy (GitProxyPath); it never holds forge
// credentials, and the proxy refuses pushes outside akili/* branches.
type ProjectSpec struct {
	ID            string `json:"id"`
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	Repo          string `json:"repo"` // owner/name, for display
	DefaultBranch string `json:"default_branch"`
	Branch        string `json:"branch"` // the session's working branch, akili/...
	// SandboxImage is the container image sandbox_exec runs in ("" = sandbox unavailable).
	SandboxImage string `json:"sandbox_image,omitempty"`
	GitName      string `json:"git_name"`
	GitEmail     string `json:"git_email"`
	// Trailers ("Key: value") are appended to every commit, linking it to its task or session.
	Trailers []string `json:"trailers,omitempty"`
}

// UserMessage injects user input into a session.
type UserMessage struct {
	Text   string        `json:"text"`
	UserID string        `json:"user_id,omitempty"`
	Images []ImageSource `json:"images,omitempty"`
}

// Message builds the user turn: images first, then the text, as providers recommend.
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

// Session states reported via TypeStatus.
const (
	StateIdle            = "idle"
	StateThinking        = "thinking"
	StateRunningTool     = "running_tool"
	StateWaitingApproval = "waiting_approval"
	// StateWaitingInput: the agent asked the user a question (ask_user) and waits for the answer.
	StateWaitingInput = "waiting_input"
)

// Status reports what a session is doing.
type Status struct {
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"`
}

// Delta kinds.
const (
	DeltaText     = "text"
	DeltaThinking = "thinking" // a summary of the model's reasoning, for display only
)

// Delta is a streamed piece of assistant output.
type Delta struct {
	Text string `json:"text"`
	Kind string `json:"kind,omitempty"` // DeltaText (default) or DeltaThinking
}

// MessageAppend adds a message to the persisted conversation.
type MessageAppend struct {
	Message Message `json:"message"`
}

// ToolRequest asks the control plane to authorise a tool call. The agent blocks the call until a
// ToolDecision with the same RequestID arrives.
type ToolRequest struct {
	RequestID string          `json:"request_id"`
	ToolUseID string          `json:"tool_use_id"`
	Tool      string          `json:"tool"`
	Input     json.RawMessage `json:"input"`
	// ChangeID and Phase mark a call the agent runs as part of an approved change plan.
	ChangeID string `json:"change_id,omitempty"`
	Phase    string `json:"phase,omitempty"`
}

// Decision effects.
const (
	EffectAllow   = "allow"
	EffectDeny    = "deny"
	EffectApprove = "approve" // needs a human; a final allow/deny follows
)

// ToolDecision answers a ToolRequest. Effect "approve" is interim: the agent keeps waiting for a
// final allow or deny.
type ToolDecision struct {
	RequestID  string `json:"request_id"`
	Effect     string `json:"effect"`
	Reason     string `json:"reason,omitempty"`
	ApprovalID string `json:"approval_id,omitempty"`
	// Result is set for remote tools: the control plane ran the call after allowing it.
	Result *RemoteResult `json:"result,omitempty"`
	// ChangeID is set when an approved change_run may start.
	ChangeID string `json:"change_id,omitempty"`
	// Deadline is the task's new deadline when this decision ends a wait on a person (approval or
	// ask_user): time spent waiting does not count toward the task timeout.
	Deadline *time.Time `json:"deadline,omitempty"`
}

// RemoteResult is the outcome of a tool the control plane executed.
type RemoteResult struct {
	Output  string `json:"output"`
	IsError bool   `json:"is_error"`
}

// ToolResult reports the outcome of an executed tool.
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

// ChangeUpdate reports a change's status as the agent runs it.
type ChangeUpdate struct {
	ChangeID string `json:"change_id"`
	Status   string `json:"status"`
	Detail   string `json:"detail,omitempty"`
}

// Terminal stream: CP opens it with pty.open, then both sides exchange data frames.
const (
	TypePTYOpen   = "pty.open"   // CP → agent (first envelope)
	TypePTYInput  = "pty.input"  // CP → agent
	TypePTYResize = "pty.resize" // CP → agent
	TypePTYOutput = "pty.output" // agent → CP
	TypePTYExit   = "pty.exit"   // agent → CP
)

// PTYOpen starts a terminal. The agent verifies the signed policy allows terminals.
type PTYOpen struct {
	TerminalID string       `json:"terminal_id"`
	Cols       int          `json:"cols"`
	Rows       int          `json:"rows"`
	Policy     SignedPolicy `json:"policy"`
	UserID     string       `json:"user_id"`
}

// PTYData carries terminal bytes.
type PTYData struct {
	Data []byte `json:"data"`
}

// PTYResize changes the terminal size.
type PTYResize struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

// PTYExit ends a terminal.
type PTYExit struct {
	Code  int    `json:"code"`
	Error string `json:"error,omitempty"`
}

// Done outcomes.
const (
	OutcomeSucceeded = "succeeded"
	OutcomeFailed    = "failed"
	OutcomeCancelled = "cancelled"
	OutcomeTimedOut  = "timed_out"
)

// Done ends a task session.
type Done struct {
	Outcome string `json:"outcome"`
	Summary string `json:"summary,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Error reports a protocol or runtime error.
type Error struct {
	Message string `json:"message"`
}
