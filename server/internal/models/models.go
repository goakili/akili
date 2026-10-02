// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package models holds the GORM models. They double as API response types; secrets are tagged
// json:"-" so they never leave the server.
package models

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/goakili/akili/proto"
	"gorm.io/gorm"
)

// NewID returns a prefixed random id such as "ag_7f3kq2...".
func NewID(prefix string) string {
	return prefix + "_" + strings.ToLower(rand.Text()[:20])
}

// Base is embedded in every tenant-owned model.
type Base struct {
	ID             string    `gorm:"primaryKey;size:40" json:"id"`
	OrganizationID string    `gorm:"size:40;index;not null" json:"organization_id"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Organization is a tenant. v2 runs a single organization, but every table is scoped.
type Organization struct {
	ID         string    `gorm:"primaryKey;size:40" json:"id"`
	Name       string    `gorm:"size:120;not null" json:"name"`
	Slug       string    `gorm:"size:120;uniqueIndex" json:"slug"`
	KillSwitch bool      `json:"kill_switch"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Roles, lowest to highest.
const (
	RoleViewer   = "viewer"
	RoleOperator = "operator"
	RoleAdmin    = "admin"
	RoleOwner    = "owner"
)

// RoleRank orders roles for RequireRole.
func RoleRank(r string) int {
	switch r {
	case RoleViewer:
		return 1
	case RoleOperator:
		return 2
	case RoleAdmin:
		return 3
	case RoleOwner:
		return 4
	}
	return 0
}

// User is a human operator.
type User struct {
	Base
	Email        string     `gorm:"size:255;uniqueIndex;not null" json:"email"`
	Name         string     `gorm:"size:120" json:"name"`
	PasswordHash string     `gorm:"size:255" json:"-"`
	Role         string     `gorm:"size:20;not null" json:"role"`
	Active       bool       `gorm:"not null;default:true" json:"active"`
	LastLoginAt  *time.Time `json:"last_login_at"`
	// SSOSubject pins the identity provider account ("issuer|sub") after the first SSO login.
	SSOSubject string `gorm:"size:255;index" json:"-"`
	// Email notifications (sent through the default Posta integration, when there is one).
	EmailApprovals bool `gorm:"not null;default:true" json:"email_approvals"`
	EmailTasks     bool `gorm:"not null;default:true" json:"email_tasks"`
}

// APIKey authenticates non-browser clients. Only the SHA-256 hash is stored.
type APIKey struct {
	Base
	UserID     string     `gorm:"size:40;index;not null" json:"user_id"`
	Name       string     `gorm:"size:120;not null" json:"name"`
	Prefix     string     `gorm:"size:16" json:"prefix"`
	Hash       string     `gorm:"size:64;uniqueIndex;not null" json:"-"`
	Scopes     []string   `gorm:"type:jsonb;serializer:json" json:"scopes"`
	LastUsedAt *time.Time `json:"last_used_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
}

// API key scopes.
const (
	ScopeRead  = "read"
	ScopeWrite = "write"
)

// Agent statuses.
const (
	AgentPending = "pending" // created, not yet enrolled
	AgentOnline  = "online"
	AgentOffline = "offline"
	AgentRevoked = "revoked"
)

// Agent is an enrolled (or pending) akili-agent.
type Agent struct {
	Base
	Name          string         `gorm:"size:120;not null" json:"name"`
	Description   string         `gorm:"size:500" json:"description"`
	Labels        []string       `gorm:"type:jsonb;serializer:json" json:"labels"`
	Status        string         `gorm:"size:20;index;not null" json:"status"`
	Draining      bool           `json:"draining"`
	PolicyID      *string        `gorm:"size:40" json:"policy_id"`
	Autonomy      proto.Autonomy `json:"autonomy"`
	ProviderID    *string        `gorm:"size:40" json:"provider_id"`
	MaxParallel   int            `gorm:"not null;default:2" json:"max_parallel"`
	Instructions  string         `gorm:"type:text" json:"instructions"`
	MonthlyBudget float64        `json:"monthly_budget_usd"`
	// GitName and GitEmail override the default commit identity; "" uses the default.
	GitName        string          `gorm:"size:120" json:"git_name"`
	GitEmail       string          `gorm:"size:254" json:"git_email"`
	PublicKey      []byte          `json:"-"`
	EnrolledAt     *time.Time      `json:"enrolled_at"`
	LastSeenAt     *time.Time      `json:"last_seen_at"`
	Version        string          `gorm:"size:40" json:"version"`
	Facts          proto.HostFacts `gorm:"type:jsonb;serializer:json" json:"facts"`
	RevokedAt      *time.Time      `json:"revoked_at"`
	CreatedBy      string          `gorm:"size:40" json:"created_by"`
	Skills         []Skill         `gorm:"many2many:agent_skills;" json:"skills,omitempty"`
	ActiveSessions int             `gorm:"-" json:"active_sessions"`
	// GitIdentity is the identity the agent commits under, with defaults applied.
	GitIdentity *GitIdentity `gorm:"-" json:"git_identity,omitempty"`
}

// GitIdentity is a git author/committer.
type GitIdentity struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func (i GitIdentity) String() string { return i.Name + " <" + i.Email + ">" }

// HasLabels reports whether the agent carries every label in want.
func (a *Agent) HasLabels(want []string) bool {
	have := map[string]bool{}
	for _, l := range a.Labels {
		have[l] = true
	}
	for _, l := range want {
		if !have[l] {
			return false
		}
	}
	return true
}

// JoinToken is a one-time enrollment secret (akj_...). Only the hash is stored.
type JoinToken struct {
	Base
	AgentID   string     `gorm:"size:40;index;not null" json:"agent_id"`
	Hash      string     `gorm:"size:64;uniqueIndex;not null" json:"-"`
	ExpiresAt time.Time  `json:"expires_at"`
	UsedAt    *time.Time `json:"used_at"`
	CreatedBy string     `gorm:"size:40" json:"created_by"`
}

// Skill is a Markdown instruction pack provisioned to agents through their system prompt.
type Skill struct {
	Base
	Builtin     bool   `json:"builtin"`
	Name        string `gorm:"size:120;not null" json:"name"`
	Description string `gorm:"size:500" json:"description"`
	Content     string `gorm:"type:text;not null" json:"content"`
	Version     int    `gorm:"not null;default:1" json:"version"`
	Hash        string `gorm:"size:64" json:"hash"`
}

// Policy is a versioned capability policy.
type Policy struct {
	Base
	Name        string       `gorm:"size:120;not null" json:"name"`
	Description string       `gorm:"size:500" json:"description"`
	Version     int          `gorm:"not null;default:1" json:"version"`
	Document    proto.Policy `gorm:"type:jsonb;serializer:json" json:"document"`
	Builtin     bool         `json:"builtin"`
}

// Provider kinds.
const (
	ProviderAnthropic = "anthropic"
	ProviderOpenAI    = "openai" // OpenAI-compatible (OpenAI, Ollama, vLLM, ...)
	ProviderFake      = "fake"   // scripted, for development and end-to-end tests
)

// ModelProvider is an LLM endpoint. The API key is encrypted at rest and never returned.
type ModelProvider struct {
	Base
	Name            string  `gorm:"size:120;not null" json:"name"`
	Kind            string  `gorm:"size:20;not null" json:"kind"`
	BaseURL         string  `gorm:"size:500" json:"base_url"`
	Model           string  `gorm:"size:120;not null" json:"model"`
	Effort          string  `gorm:"size:20" json:"effort"`
	MaxTokens       int     `json:"max_tokens"`
	APIKeyEnc       string  `gorm:"type:text" json:"-"`
	HasKey          bool    `gorm:"-" json:"has_key"`
	IsDefault       bool    `json:"is_default"`
	InputPriceMTok  float64 `json:"input_price_mtok"`
	OutputPriceMTok float64 `json:"output_price_mtok"`
}

// AfterFind fills HasKey.
func (p *ModelProvider) AfterFind(*gorm.DB) error {
	p.HasKey = p.APIKeyEnc != ""
	return nil
}

// Integration kinds: git forges, the Miabi PaaS and Posta (email notifications).
const (
	ForgeGitea  = "gitea"
	ForgeGitHub = "github"
	KindMiabi   = "miabi"
	KindPosta   = "posta"
)

// Integration auth types.
const (
	AuthToken     = "token"      // a personal/bot access token
	AuthGitHubApp = "github_app" // GitHub App installation tokens, minted per use (short-lived)
)

// Integration is a connection to a git forge. Secrets are encrypted at rest and never returned; the
// agent never sees them either: git traffic and forge API calls go through the control plane.
type Integration struct {
	Base
	Name     string `gorm:"size:120;not null" json:"name"`
	Kind     string `gorm:"size:20;not null" json:"kind"`
	BaseURL  string `gorm:"size:500;not null" json:"base_url"` // e.g. https://gitea.example.com or https://api.github.com
	WebURL   string `gorm:"size:500" json:"web_url"`           // clone/web host, e.g. https://github.com
	AuthType string `gorm:"size:20;not null" json:"auth_type"`
	Username string `gorm:"size:120" json:"username"` // token owner (Gitea basic auth)
	// Workspace is the Miabi workspace (id, uid or handle) a Miabi integration operates in.
	Workspace string `gorm:"size:120" json:"workspace"`
	// CACert is an extra CA (PEM) to trust for BaseURL: a self-signed or private-CA Miabi. Public.
	CACert string `gorm:"type:text" json:"ca_cert"`
	// Sender is the From address of a Posta integration, e.g. "Akili <akili@example.com>".
	Sender string `gorm:"size:320" json:"sender"`
	// Default marks the integration used when none is named: Miabi tool calls, Posta notification
	// email (one per kind and organization).
	Default bool `gorm:"column:is_default;not null;default:false" json:"default"`
	// Token auth.
	TokenEnc string `gorm:"type:text" json:"-"`
	// GitHub App auth.
	AppID          int64  `json:"app_id,omitempty"`
	InstallationID int64  `json:"installation_id,omitempty"`
	PrivateKeyEnc  string `gorm:"type:text" json:"-"`
	HasSecret      bool   `gorm:"-" json:"has_secret"`
	// WebhookSecretEnc verifies forge webhooks (issue triggers).
	WebhookSecretEnc string `gorm:"type:text" json:"-"`
	CreatedBy        string `gorm:"size:40" json:"created_by"`
}

// AfterFind fills HasSecret.
func (i *Integration) AfterFind(*gorm.DB) error {
	i.HasSecret = i.TokenEnc != "" || i.PrivateKeyEnc != ""
	return nil
}

// Project is a repository agents work on.
type Project struct {
	Base
	Name          string   `gorm:"size:120;not null" json:"name"`
	Slug          string   `gorm:"size:120;not null" json:"slug"`
	Description   string   `gorm:"size:500" json:"description"`
	IntegrationID string   `gorm:"size:40;index;not null" json:"integration_id"`
	Forge         string   `gorm:"size:20" json:"forge"` // gitea | github, copied from the integration
	Owner         string   `gorm:"size:120;not null" json:"owner"`
	Repo          string   `gorm:"size:120;not null" json:"repo"`
	DefaultBranch string   `gorm:"size:120;not null" json:"default_branch"`
	WebURL        string   `gorm:"size:500" json:"web_url"`
	AgentID       *string  `gorm:"size:40" json:"agent_id"` // preferred agent for its tasks
	Selector      []string `gorm:"type:jsonb;serializer:json" json:"selector"`
	// SandboxImage enables sandbox_exec (tests run in a disposable container), e.g. golang:1.26.
	SandboxImage string `gorm:"size:200" json:"sandbox_image"`
	// Instructions are project conventions added to every session's system prompt.
	Instructions string `gorm:"type:text" json:"instructions"`
	// TriggerLabel creates a task when an issue gets this label (forge webhook); "" disables.
	TriggerLabel string `gorm:"size:60" json:"trigger_label"`
	CreatedBy    string `gorm:"size:40" json:"created_by"`
}

// FullName is owner/repo.
func (p *Project) FullName() string { return p.Owner + "/" + p.Repo }

// ChangeCall is one call of a change plan with its outcome.
type ChangeCall struct {
	Phase       string          `json:"phase"` // step | verify | rollback
	Tool        string          `json:"tool"`
	Input       json.RawMessage `json:"input"`
	Description string          `json:"description,omitempty"`
	Expect      string          `json:"expect,omitempty"`
	Reject      string          `json:"reject,omitempty"`
	Hash        string          `json:"hash"`
	RequestID   string          `json:"request_id,omitempty"` // the agent request that ran it
	Risk        string          `json:"risk"`
	Status      string          `json:"status"` // pending | ok | failed | skipped
	Output      string          `json:"output,omitempty"`
	DurationMs  int64           `json:"duration_ms,omitempty"`
}

// Change is a change plan proposed by an agent, approved by a human as a whole, then executed with
// verification and automatic rollback.
type Change struct {
	Base
	SessionID  string       `gorm:"size:40;index;not null" json:"session_id"`
	TaskID     *string      `gorm:"size:40;index" json:"task_id"`
	AgentID    string       `gorm:"size:40;index;not null" json:"agent_id"`
	ApprovalID string       `gorm:"size:40;index" json:"approval_id"`
	Title      string       `gorm:"size:300;not null" json:"title"`
	Reason     string       `gorm:"type:text" json:"reason"`
	Risk       string       `gorm:"size:20" json:"risk"`
	Status     string       `gorm:"size:20;index;not null" json:"status"`
	Detail     string       `gorm:"size:500" json:"detail"`
	Calls      []ChangeCall `gorm:"type:jsonb;serializer:json" json:"calls"`
	StartedAt  *time.Time   `json:"started_at"`
	FinishedAt *time.Time   `json:"finished_at"`
}

// AlertRoute turns incoming alerts (Alertmanager or generic JSON) into triage tasks.
type AlertRoute struct {
	Base
	Name        string   `gorm:"size:120;not null" json:"name"`
	TokenHash   string   `gorm:"size:64;uniqueIndex;not null" json:"-"`
	TokenPrefix string   `gorm:"size:16" json:"token_prefix"`
	Enabled     bool     `json:"enabled"`
	AgentID     *string  `gorm:"size:40" json:"agent_id"`
	Selector    []string `gorm:"type:jsonb;serializer:json" json:"selector"`
	// HostLabel names the alert label holding the affected host (default "instance"); an agent whose
	// name or hostname matches it is targeted directly.
	HostLabel string            `gorm:"size:60" json:"host_label"`
	Match     map[string]string `gorm:"type:jsonb;serializer:json" json:"match"` // required label values
	Autonomy  proto.Autonomy    `json:"autonomy"`
	// Instructions are added to every triage goal (e.g. escalation contacts, runbook hints).
	Instructions string     `gorm:"type:text" json:"instructions"`
	LastAlertAt  *time.Time `json:"last_alert_at"`
	CreatedBy    string     `gorm:"size:40" json:"created_by"`
}

// MiabiWatch reacts to a Miabi app's events: verify each deploy, triage failures.
type MiabiWatch struct {
	Base
	IntegrationID string `gorm:"size:40;index;not null" json:"integration_id"`
	// Workspace is the Miabi workspace handle; App is an app name or a pattern ("*", "api-*").
	Workspace string   `gorm:"size:64;index" json:"workspace"`
	App       string   `gorm:"size:128;not null" json:"app"`
	AppID     int64    `json:"app_id"` // set when App names a single app
	AgentID   *string  `gorm:"size:40" json:"agent_id"`
	Selector  []string `gorm:"type:jsonb;serializer:json" json:"selector"`
	// VerifyDeploys runs a post-deploy check on every successful deploy (and rolls back on failure).
	VerifyDeploys bool `json:"verify_deploys"`
	// TriageFailures opens a triage task on failed deploys, crashed containers and drift.
	TriageFailures bool `json:"triage_failures"`
	// Databases opens triage tasks for failed backups, restores, provisioning and upgrades.
	Databases    bool           `json:"databases"`
	HealthURL    string         `gorm:"size:500" json:"health_url"`
	Autonomy     proto.Autonomy `json:"autonomy"`
	Instructions string         `gorm:"type:text" json:"instructions"`
	LastEventAt  *time.Time     `json:"last_event_at"`
	CreatedBy    string         `gorm:"size:40" json:"created_by"`
}

// MiabiWorkspace is a workspace a Miabi integration's key can reach. Only enabled ones are usable
// by agents and followed for events.
type MiabiWorkspace struct {
	Base
	IntegrationID string `gorm:"size:40;uniqueIndex:miabi_ws;not null" json:"integration_id"`
	Name          string `gorm:"size:64;uniqueIndex:miabi_ws;not null" json:"name"` // the handle
	MiabiID       int64  `gorm:"index" json:"miabi_id"`
	DisplayName   string `gorm:"size:200" json:"display_name"`
	Role          string `gorm:"size:20" json:"role"`
	// Accessible is false when the key is bound to another workspace.
	Accessible bool `json:"accessible"`
	Enabled    bool `json:"enabled"`
	// EventCursor is the last event id handled, so a reconnect or a new leader resumes without gaps.
	EventCursor int64      `json:"-"`
	Streaming   bool       `gorm:"-" json:"streaming"`
	LastEventAt *time.Time `json:"last_event_at"`
	LastError   string     `gorm:"size:500" json:"last_error"`
	SyncedAt    *time.Time `json:"synced_at"`
}

// MCPServer is an MCP server the control plane connects to; its tools become remote tools named
// "mcp__<name>__<tool>". Stdio servers run as child processes of the control plane, limited to the
// commands in AKILI_MCP_COMMANDS.
type MCPServer struct {
	Base
	Name      string   `gorm:"size:32;uniqueIndex;not null" json:"name"`
	Transport string   `gorm:"size:10;not null" json:"transport"` // stdio | http
	Command   string   `gorm:"size:200" json:"command"`
	Args      []string `gorm:"type:jsonb;serializer:json" json:"args"`
	URL       string   `gorm:"size:500" json:"url"`
	// EnvEnc holds the encrypted environment (stdio) or headers (http) as a JSON object; only the
	// names are shown (EnvKeys).
	EnvEnc  string   `gorm:"type:text" json:"-"`
	EnvKeys []string `gorm:"type:jsonb;serializer:json" json:"env_keys"`
	// IntegrationID makes a Miabi preset: MIABI_SERVER and MIABI_TOKEN come from the integration.
	IntegrationID *string    `gorm:"size:40" json:"integration_id"`
	AllowWrite    bool       `json:"allow_write"`
	Enabled       bool       `json:"enabled"`
	LastError     string     `gorm:"size:500" json:"last_error"`
	SyncedAt      *time.Time `json:"synced_at"`
	CreatedBy     string     `gorm:"size:40" json:"created_by"`
}

// MCPTool is a tool discovered on an MCP server. Agents get it only when enabled with a risk.
type MCPTool struct {
	Base
	ServerID    string          `gorm:"size:40;uniqueIndex:mcp_tool;not null" json:"server_id"`
	Name        string          `gorm:"size:64;uniqueIndex:mcp_tool;not null" json:"name"`
	Description string          `gorm:"type:text" json:"description"`
	InputSchema json.RawMessage `gorm:"type:jsonb" json:"input_schema"`
	ReadOnly    bool            `json:"read_only"`
	Destructive bool            `json:"destructive"`
	Enabled     bool            `json:"enabled"`
	Risk        string          `gorm:"size:10" json:"risk"` // low | medium | high | critical; empty until set
}

// Lesson statuses.
const (
	LessonProposed = "proposed"
	LessonApproved = "approved"
	LessonRejected = "rejected"
)

// Lesson is a durable note for future sessions ("soul"). Agents only propose; a lesson reaches
// system prompts after an operator approves it, so injected text cannot quietly change behaviour.
type Lesson struct {
	Base
	AgentID    *string    `gorm:"size:40;index" json:"agent_id"` // nil: applies to every agent
	Text       string     `gorm:"size:500;not null" json:"text"`
	Status     string     `gorm:"size:20;index;not null" json:"status"`
	SessionID  *string    `gorm:"size:40" json:"session_id"` // where it was proposed
	TaskID     *string    `gorm:"size:40" json:"task_id"`
	ProposedBy string     `gorm:"size:40" json:"proposed_by"` // agent or user id
	DecidedBy  *string    `gorm:"size:40" json:"decided_by"`
	DecidedAt  *time.Time `json:"decided_at"`
	Note       string     `gorm:"size:500" json:"note"`
}

// Chat channel kinds.
const (
	ChatSlack    = "slack"
	ChatTelegram = "telegram"
	ChatSignal   = "signal"
)

// ChatChannel is a chat platform connection (a Slack app, a Telegram bot, a Signal number).
type ChatChannel struct {
	Base
	Name    string `gorm:"size:120;not null" json:"name"`
	Kind    string `gorm:"size:20;not null" json:"kind"`
	Enabled bool   `json:"enabled"`
	// APIBaseURL overrides the platform API (a signal-cli REST server's URL; tests).
	APIBaseURL string `gorm:"size:500" json:"api_base_url"`
	// Account is the Signal number the bot sends from.
	Account string `gorm:"size:64" json:"account"`
	// TokenEnc is the bot token (Slack bot token, Telegram bot token); SecretEnc the Slack signing secret.
	TokenEnc       string  `gorm:"type:text" json:"-"`
	SecretEnc      string  `gorm:"type:text" json:"-"`
	DefaultAgentID *string `gorm:"size:40" json:"default_agent_id"`
	// Cursor is the platform's update offset (Telegram), so a new leader does not replay messages.
	Cursor    int64      `json:"-"`
	LastError string     `gorm:"size:500" json:"last_error"`
	LastSeen  *time.Time `json:"last_seen_at"`
	CreatedBy string     `gorm:"size:40" json:"created_by"`
	HasToken  bool       `gorm:"-" json:"has_token"`
	HasSecret bool       `gorm:"-" json:"has_secret"`
}

// AfterFind reports which credentials are set without exposing them.
func (c *ChatChannel) AfterFind(*gorm.DB) error {
	c.HasToken, c.HasSecret = c.TokenEnc != "", c.SecretEnc != ""
	return nil
}

// ChatIdentity links a chat user to an Akili user: chat actions run with that user's role.
type ChatIdentity struct {
	Base
	ChannelID   string     `gorm:"size:40;uniqueIndex:chat_identity;not null" json:"channel_id"`
	ExternalID  string     `gorm:"size:128;uniqueIndex:chat_identity;not null" json:"external_id"`
	DisplayName string     `gorm:"size:200" json:"display_name"`
	UserID      string     `gorm:"size:40;index;not null" json:"user_id"`
	LastUsedAt  *time.Time `json:"last_used_at"`
}

// ChatLinkCode is a one-time code an Akili user sends to the bot to link their chat account.
type ChatLinkCode struct {
	Base
	UserID    string    `gorm:"size:40;not null" json:"user_id"`
	Hash      string    `gorm:"size:64;uniqueIndex;not null" json:"-"`
	ExpiresAt time.Time `json:"expires_at"`
}

// ChatConversation is one chat (a DM, group or channel) and the agent session it talks to.
type ChatConversation struct {
	Base
	ChannelID  string  `gorm:"size:40;uniqueIndex:chat_conv;not null" json:"channel_id"`
	ExternalID string  `gorm:"size:128;uniqueIndex:chat_conv;not null" json:"external_id"`
	AgentID    *string `gorm:"size:40" json:"agent_id"`
	SessionID  *string `gorm:"size:40;index" json:"session_id"`
}

// TerminalSession is a recorded interactive terminal on an agent.
type TerminalSession struct {
	Base
	AgentID   string     `gorm:"size:40;index;not null" json:"agent_id"`
	UserID    string     `gorm:"size:40;index;not null" json:"user_id"`
	Status    string     `gorm:"size:20;not null" json:"status"` // open | closed
	Cols      int        `json:"cols"`
	Rows      int        `json:"rows"`
	EndedAt   *time.Time `json:"ended_at"`
	ExitCode  int        `json:"exit_code"`
	Bytes     int        `json:"bytes"`
	Truncated bool       `json:"truncated"`
	// Recording is an asciinema v2 cast (output and input events).
	Recording string `gorm:"type:text" json:"-"`
}

// Session statuses and modes.
const (
	SessionOpen   = "open"
	SessionClosed = "closed"
)

// ChatSession is a conversation with an agent: interactive chat, or the execution of a task.
type ChatSession struct {
	Base
	AgentID        string     `gorm:"size:40;index;not null" json:"agent_id"`
	TaskID         *string    `gorm:"size:40;index" json:"task_id"`
	ProjectID      *string    `gorm:"size:40;index" json:"project_id"`
	Branch         string     `gorm:"size:200" json:"branch"`
	Title          string     `gorm:"size:200" json:"title"`
	Mode           string     `gorm:"size:10;not null" json:"mode"`
	Status         string     `gorm:"size:10;not null" json:"status"`
	State          string     `gorm:"size:30" json:"state"`
	CreatedBy      string     `gorm:"size:40" json:"created_by"`
	LastActivityAt *time.Time `json:"last_activity_at"`
	InputTokens    int        `json:"input_tokens"`
	OutputTokens   int        `json:"output_tokens"`
	CostUSD        float64    `json:"cost_usd"`
	// System and ToolNames are fixed when the session first opens: providers bind reasoning to the
	// exact prompt and tool set, so they must not change for the life of the conversation.
	System    string   `gorm:"type:text" json:"-"`
	ToolNames []string `gorm:"type:jsonb;serializer:json" json:"tool_names"`
}

// SessionMessage is one persisted conversation turn. The history is append-only: it is replayed to
// the provider exactly as stored.
type SessionMessage struct {
	ID        uint          `gorm:"primaryKey" json:"id"`
	SessionID string        `gorm:"size:40;index;not null" json:"session_id"`
	Role      string        `gorm:"size:20;not null" json:"role"`
	Content   []proto.Block `gorm:"type:jsonb;serializer:json" json:"content"`
	CreatedAt time.Time     `json:"created_at"`
}

// Attachment is an image a user attached to a session. Messages reference it by id, so the bytes stay
// out of the history that is replayed to agents on every reopen.
type Attachment struct {
	Base
	SessionID string `gorm:"size:40;index;not null" json:"session_id"`
	MediaType string `gorm:"size:40;not null" json:"media_type"`
	Size      int    `json:"size"`
	CreatedBy string `gorm:"size:40" json:"created_by"`
	Data      []byte `gorm:"not null" json:"-"`
}

// SessionEvent is a persisted timeline entry (tool calls, approvals, status) for replay in the UI.
type SessionEvent struct {
	ID        uint            `gorm:"primaryKey" json:"id"`
	SessionID string          `gorm:"size:40;index;not null" json:"session_id"`
	Type      string          `gorm:"size:40;not null" json:"type"`
	Payload   json.RawMessage `gorm:"type:jsonb" json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

// Approval statuses.
const (
	ApprovalPending  = "pending"
	ApprovalApproved = "approved"
	ApprovalDenied   = "denied"
	ApprovalExpired  = "expired"
)

// Approval is a human decision on one exact tool call.
type Approval struct {
	Base
	SessionID string          `gorm:"size:40;index;not null" json:"session_id"`
	TaskID    *string         `gorm:"size:40" json:"task_id"`
	AgentID   string          `gorm:"size:40;index;not null" json:"agent_id"`
	RequestID string          `gorm:"size:60;not null" json:"request_id"`
	Tool      string          `gorm:"size:60;not null" json:"tool"`
	Input     json.RawMessage `gorm:"type:jsonb" json:"input"`
	InputHash string          `gorm:"size:64;not null" json:"input_hash"`
	Risk      string          `gorm:"size:20" json:"risk"`
	Reason    string          `gorm:"size:500" json:"reason"`
	Status    string          `gorm:"size:20;index;not null" json:"status"`
	DecidedBy *string         `gorm:"size:40" json:"decided_by"`
	DecidedAt *time.Time      `json:"decided_at"`
	Note      string          `gorm:"size:500" json:"note"`
	ExpiresAt time.Time       `json:"expires_at"`
	ChangeID  *string         `gorm:"size:40" json:"change_id"` // set for change_run approvals
}

// Task statuses.
const (
	TaskQueued    = "queued"
	TaskAssigned  = "assigned"
	TaskRunning   = "running"
	TaskSucceeded = "succeeded"
	TaskFailed    = "failed"
	TaskCancelled = "cancelled"
	TaskTimedOut  = "timed_out"
)

// TaskTerminal reports whether a status is final.
func TaskTerminal(s string) bool {
	return s == TaskSucceeded || s == TaskFailed || s == TaskCancelled || s == TaskTimedOut
}

// Task is a unit of autonomous work assigned to an agent.
type Task struct {
	Base
	Title           string         `gorm:"size:200;not null" json:"title"`
	Goal            string         `gorm:"type:text;not null" json:"goal"`
	AgentID         *string        `gorm:"size:40" json:"agent_id"`
	Selector        []string       `gorm:"type:jsonb;serializer:json" json:"selector"`
	AssignedAgentID *string        `gorm:"size:40;index" json:"assigned_agent_id"`
	SessionID       *string        `gorm:"size:40" json:"session_id"`
	Status          string         `gorm:"size:20;index;not null" json:"status"`
	StatusReason    string         `gorm:"size:300" json:"status_reason"`
	Priority        int            `gorm:"not null;default:0" json:"priority"`
	Autonomy        proto.Autonomy `json:"autonomy"`
	BudgetUSD       float64        `json:"budget_usd"`
	MaxTurns        int            `json:"max_turns"`
	TimeoutSec      int            `json:"timeout_sec"`
	Attempts        int            `json:"attempts"`
	MaxAttempts     int            `json:"max_attempts"`
	LeaseUntil      *time.Time     `json:"lease_until"`
	StartedAt       *time.Time     `json:"started_at"`
	FinishedAt      *time.Time     `json:"finished_at"`
	Result          string         `gorm:"type:text" json:"result"`
	Error           string         `gorm:"type:text" json:"error"`
	ScheduleID      *string        `gorm:"size:40;index" json:"schedule_id"`
	CreatedBy       string         `gorm:"size:40" json:"created_by"`
	CostUSD         float64        `json:"cost_usd"`
	// Coding tasks.
	ProjectID *string `gorm:"size:40;index" json:"project_id"`
	Branch    string  `gorm:"size:200" json:"branch"`
	PRNumber  int     `json:"pr_number"`
	PRURL     string  `gorm:"size:500" json:"pr_url"`
	// Trigger records what created the task (manual, schedule, issue).
	Trigger    string `gorm:"size:40" json:"trigger"`
	TriggerRef string `gorm:"size:300" json:"trigger_ref"`
}

// TaskTemplate is what a schedule creates.
type TaskTemplate struct {
	Title       string         `json:"title"`
	Goal        string         `json:"goal"`
	AgentID     *string        `json:"agent_id,omitempty"`
	Selector    []string       `json:"selector,omitempty"`
	Priority    int            `json:"priority"`
	Autonomy    proto.Autonomy `json:"autonomy"`
	BudgetUSD   float64        `json:"budget_usd"`
	MaxTurns    int            `json:"max_turns"`
	TimeoutSec  int            `json:"timeout_sec"`
	MaxAttempts int            `json:"max_attempts"`
	ProjectID   *string        `json:"project_id,omitempty"`
}

// Schedule creates tasks on a cron expression.
type Schedule struct {
	Base
	Name      string       `gorm:"size:120;not null" json:"name"`
	Cron      string       `gorm:"size:120;not null" json:"cron"`
	Enabled   bool         `json:"enabled"`
	Template  TaskTemplate `gorm:"type:jsonb;serializer:json" json:"template"`
	NextRunAt *time.Time   `gorm:"index" json:"next_run_at"`
	LastRunAt *time.Time   `json:"last_run_at"`
	CreatedBy string       `gorm:"size:40" json:"created_by"`
}

// Usage records one model call.
type Usage struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	OrganizationID string    `gorm:"size:40;index;not null" json:"organization_id"`
	AgentID        string    `gorm:"size:40;index" json:"agent_id"`
	SessionID      string    `gorm:"size:40;index" json:"session_id"`
	TaskID         *string   `gorm:"size:40;index" json:"task_id"`
	ProviderID     string    `gorm:"size:40" json:"provider_id"`
	Model          string    `gorm:"size:120" json:"model"`
	InputTokens    int       `json:"input_tokens"`
	OutputTokens   int       `json:"output_tokens"`
	CostUSD        float64   `json:"cost_usd"`
	CreatedAt      time.Time `gorm:"index" json:"created_at"`
}

// AuditLog is the append-only, hash-chained audit trail.
type AuditLog struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	OrganizationID string         `gorm:"size:40;index;not null" json:"organization_id"`
	CreatedAt      time.Time      `gorm:"index" json:"created_at"`
	ActorType      string         `gorm:"size:20;not null" json:"actor_type"`
	ActorID        string         `gorm:"size:40;index" json:"actor_id"`
	Action         string         `gorm:"size:80;index;not null" json:"action"`
	TargetType     string         `gorm:"size:40" json:"target_type"`
	TargetID       string         `gorm:"size:60;index" json:"target_id"`
	IP             string         `gorm:"size:64" json:"ip"`
	Metadata       map[string]any `gorm:"type:jsonb;serializer:json" json:"metadata"`
	PrevHash       string         `gorm:"size:64" json:"prev_hash"`
	Hash           string         `gorm:"size:64;not null" json:"hash"`
}

// ErrAuditAppendOnly is returned by any attempt to change or delete an audit row through GORM.
var ErrAuditAppendOnly = errors.New("audit_logs is append-only")

// BeforeUpdate refuses updates: the audit log is append-only.
func (*AuditLog) BeforeUpdate(*gorm.DB) error { return ErrAuditAppendOnly }

// BeforeDelete refuses deletes: the audit log is append-only.
func (*AuditLog) BeforeDelete(*gorm.DB) error { return ErrAuditAppendOnly }

// SchemaMigration records an applied versioned migration step.
type SchemaMigration struct {
	ID        string `gorm:"primaryKey;size:120"`
	AppliedAt time.Time
}

// License is the installed Enterprise license. The signed token is authoritative; the other columns
// are for display and are re-derived from it on every start.
type License struct {
	ID        uint      `gorm:"primaryKey" json:"-"`
	LicenseID string    `gorm:"size:80" json:"license_id"`
	Customer  string    `gorm:"size:200" json:"customer"`
	Token     string    `gorm:"type:text;not null" json:"-"`
	NotAfter  time.Time `json:"not_after"`
	CreatedAt time.Time `json:"created_at"`
}

// Setting is a key/value row for control-plane state (e.g. the encrypted policy-signing key).
type Setting struct {
	Key       string `gorm:"primaryKey;size:80"`
	Value     string `gorm:"type:text"`
	UpdatedAt time.Time
}

// All lists every model for migrations.
func All() []any {
	return []any{
		&Organization{}, &User{}, &APIKey{}, &Agent{}, &JoinToken{}, &Skill{}, &Policy{}, &ModelProvider{},
		&ChatSession{}, &SessionMessage{}, &SessionEvent{}, &Attachment{}, &Approval{}, &Task{}, &Schedule{}, &Usage{},
		&AuditLog{}, &Setting{}, &SchemaMigration{}, &Integration{}, &Project{}, &Change{}, &AlertRoute{}, &TerminalSession{}, &MiabiWatch{},
		&MiabiWorkspace{}, &MCPServer{}, &MCPTool{}, &Lesson{}, &ChatChannel{}, &ChatIdentity{}, &ChatLinkCode{}, &ChatConversation{}, &License{},
	}
}
