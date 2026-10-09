// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Typed client for the Akili control-plane API (/api/v1). Models mirror the Go JSON tags in
// server/internal/models, server/internal/handlers and proto/.

// ---- shared scalars -------------------------------------------------------------------------------

export type ISODate = string
export type Role = 'viewer' | 'operator' | 'admin' | 'owner'
export type Autonomy = 0 | 1 | 2 | 3
export type Risk = 'low' | 'medium' | 'high' | 'critical'
export type Effect = 'allow' | 'deny' | 'approve'
export type AgentStatus = 'pending' | 'online' | 'offline' | 'revoked'
export type TaskStatus = 'draft' | 'queued' | 'assigned' | 'running' | 'succeeded' | 'failed' | 'cancelled' | 'timed_out'
export type ApprovalStatus = 'pending' | 'approved' | 'denied' | 'expired'
export type SessionState = 'idle' | 'thinking' | 'running_tool' | 'waiting_approval' | 'waiting_input' | ''
export type ProviderKind = 'anthropic' | 'openai' | 'fake'
export type Effort = '' | 'low' | 'medium' | 'high' | 'xhigh' | 'max'

export const ROLES: Role[] = ['viewer', 'operator', 'admin', 'owner']
export const RISKS: Risk[] = ['low', 'medium', 'high', 'critical']

export function roleRank(r: string | undefined | null): number {
  const i = ROLES.indexOf(r as Role)
  return i < 0 ? 0 : i + 1
}

export const AUTONOMY_LEVELS: { value: Autonomy; label: string; help: string }[] = [
  { value: 0, label: 'L0 · Suggest only', help: 'Every tool call needs approval.' },
  { value: 1, label: 'L1 · Auto low', help: 'Runs low-risk tools on its own.' },
  { value: 2, label: 'L2 · Auto medium', help: 'Runs low and medium-risk tools on its own.' },
  { value: 3, label: 'L3 · Auto high', help: 'Runs up to high-risk tools on its own.' },
]

// ---- models ----------------------------------------------------------------------------------------

interface Base {
  id: string
  organization_id: string
  created_at: ISODate
  updated_at: ISODate
}

export interface Organization {
  id: string
  name: string
  slug: string
  kill_switch: boolean
  created_at: ISODate
  updated_at: ISODate
}

export interface User extends Base {
  email: string
  name: string
  role: Role
  active: boolean
  last_login_at: ISODate | null
  totp_enabled: boolean
}

export interface APIKey extends Base {
  user_id: string
  name: string
  prefix: string
  scopes: string[] | null
  last_used_at: ISODate | null
  expires_at: ISODate | null
  revoked_at: ISODate | null
}

export interface HostFacts {
  hostname: string
  os: string
  arch: string
  kernel?: string
  cpus: number
  mem_total_mb?: number
  mem_avail_mb?: number
  load1?: number
  uptime_sec?: number
  ips?: string[]
  agent_version: string
  workdir?: string
}

export interface Skill extends Base {
  /** Seeded by Akili (a runbook); read-only. */
  builtin?: boolean
  name: string
  description: string
  content: string
  version: number
  hash: string
}

export interface Agent extends Base {
  name: string
  description: string
  labels: string[] | null
  status: AgentStatus
  draining: boolean
  policy_id: string | null
  autonomy: Autonomy
  provider_id: string | null
  max_parallel: number
  instructions: string
  monthly_budget_usd: number
  git_name: string
  git_email: string
  /** The identity the agent commits under, with defaults applied (single-agent reads only). */
  git_identity?: { name: string; email: string }
  enrolled_at: ISODate | null
  last_seen_at: ISODate | null
  version: string
  facts: HostFacts
  revoked_at: ISODate | null
  created_by: string
  skills?: Skill[]
  active_sessions: number
}

export interface Enrollment {
  agent: Agent
  join_token: string
  expires_at: ISODate
  install_command: string
  docker_command: string
}

export interface Rule {
  allow?: string[] | null
  deny?: string[] | null
}

export interface PolicyDocument {
  name: string
  version: number
  tools: Rule
  paths: Rule
  commands: Rule
  domains: Rule
  /** systemd units the service tools may touch. */
  services?: Rule
  /** Container names the docker tools may touch. */
  containers?: Rule
  /** Miabi resources the miabi tools may touch: workspace/app, workspace/db:<name>, workspace/* … */
  apps?: Rule
  /** Operators (admins) may open a recorded interactive terminal on the agent. */
  terminal?: boolean
  max_risk: Risk | 'unknown' | ''
  require_approval?: string[] | null
  allow_shell_meta?: boolean
}

export interface Policy extends Base {
  name: string
  description: string
  version: number
  document: PolicyDocument
  builtin: boolean
}

export interface Resources {
  paths?: string[]
  commands?: string[]
  domains?: string[]
  services?: string[]
  containers?: string[]
  apps?: string[]
}

export interface Decision {
  effect: Effect
  reason: string
  risk: Risk | 'unknown'
  resources: Resources
}

export interface ToolSpec {
  name: string
  description: string
  risk: Risk
  input_schema: JSONSchema | null
  /** Runs on the control plane (forge calls), not on the agent host. */
  remote?: boolean
  /** Needs a session bound to a project workspace. */
  project?: boolean
}

export interface JSONSchema {
  type?: string
  properties?: Record<string, JSONSchema & { description?: string }>
  required?: string[]
  description?: string
  items?: JSONSchema
}

export interface ModelProvider extends Base {
  name: string
  kind: ProviderKind
  base_url: string
  model: string
  effort: Effort
  max_tokens: number
  /** Context window in tokens; 0 = 200,000. */
  context_tokens: number
  has_key: boolean
  is_default: boolean
  input_price_mtok: number
  output_price_mtok: number
}

export interface ChatSession extends Base {
  agent_id: string
  task_id: string | null
  title: string
  mode: 'chat' | 'task'
  status: 'open' | 'closed'
  state: SessionState
  created_by: string
  last_activity_at: ISODate | null
  input_tokens: number
  output_tokens: number
  cost_usd: number
  tool_names: string[] | null
  project_id: string | null
  branch: string
}

export type BlockType = 'text' | 'tool_use' | 'tool_result' | 'thinking' | 'redacted_thinking' | 'image'

export interface Block {
  type: BlockType | string
  text?: string
  id?: string
  name?: string
  input?: unknown
  tool_use_id?: string
  content?: string
  is_error?: boolean
  thinking?: string
  signature?: string
  data?: string
  /** image: a reference to the session attachment holding the bytes. */
  source?: { attachment_id: string; media_type: string }
}

export interface Attachment {
  id: string
  session_id: string
  media_type: string
  size: number
  created_at: ISODate
}

// ---- edition and license ----------------------------------------------------------------------

export interface LicenseFeature {
  name: string
  description: string
  granted: boolean
}

export type LicenseState = 'valid' | 'grace' | 'degraded' | 'none' | 'binding_mismatch'

export interface LicenseInfo {
  edition: 'community' | 'enterprise'
  state: LicenseState
  customer?: string
  license_id?: string
  /** This deployment's Install ID; customers quote it to get a license bound to it. */
  install_id: string
  /** The Install ID the installed license is bound to, if any. */
  license_install_id?: string
  url?: string
  /** False for a Community build or one without a license public key. */
  licensable: boolean
  binding_error?: string
  flags: Record<string, boolean>
  limits: Record<string, number>
  not_after?: ISODate
  grace_ends?: ISODate
  features: LicenseFeature[]
  agents_in_use: number
}

/** Accepted image types and limits; the control plane enforces the same. */
export const IMAGE_TYPES = ['image/png', 'image/jpeg', 'image/gif', 'image/webp']
export const MAX_IMAGE_BYTES = 5 * 1024 * 1024
export const MAX_IMAGES_PER_MESSAGE = 5

export interface SessionMessage {
  id: number
  session_id: string
  role: 'user' | 'assistant'
  content: Block[] | null
  created_at: ISODate
}

export interface SessionEvent {
  id: number
  session_id: string
  type: string
  payload: unknown
  created_at: ISODate
}

export type QuestionStatus = 'pending' | 'answered' | 'expired'

/** An agent's ask_user call: pick one of its options or answer in your own words. */
export interface Question extends Base {
  session_id: string
  task_id: string | null
  agent_id: string
  request_id: string
  question: string
  options: { label: string; description?: string; recommended?: boolean }[] | null
  status: QuestionStatus
  /** 0-based option picked; null when answered in own words. */
  choice: number | null
  answer: string
  answered_by: string | null
  answered_at: ISODate | null
  expires_at: ISODate
}

export interface Approval extends Base {
  session_id: string
  task_id: string | null
  /** Set for change_run approvals: the change plan the approval decides. */
  change_id?: string | null
  agent_id: string
  request_id: string
  tool: string
  input: unknown
  input_hash: string
  risk: Risk | string
  reason: string
  status: ApprovalStatus
  decided_by: string | null
  decided_at: ISODate | null
  note: string
  expires_at: ISODate
}

export interface SessionDetail {
  session: ChatSession
  messages: SessionMessage[] | null
  events: SessionEvent[] | null
  approvals: Approval[] | null
  questions: Question[] | null
  project?: Project | null
}

export interface Task extends Base {
  title: string
  goal: string
  agent_id: string | null
  selector: string[] | null
  assigned_agent_id: string | null
  session_id: string | null
  status: TaskStatus
  status_reason: string
  priority: number
  autonomy: Autonomy
  budget_usd: number
  max_turns: number
  timeout_sec: number
  attempts: number
  max_attempts: number
  lease_until: ISODate | null
  /** Set while the task waits on a person; the timeout clock is stopped. */
  paused_at: ISODate | null
  /** Seconds spent waiting on people so far (not counted toward the timeout). */
  paused_sec: number
  started_at: ISODate | null
  finished_at: ISODate | null
  result: string
  error: string
  schedule_id: string | null
  created_by: string
  cost_usd: number
  // coding tasks
  project_id: string | null
  branch: string
  pr_number: number
  pr_url: string
  /** What created the task: manual, schedule, issue, template, alert, miabi, chat. */
  trigger: string
  trigger_ref: string
  /** Project plans linked to the task. */
  plan_ids?: string[] | null
}

export interface TaskTemplate {
  title: string
  goal: string
  agent_id?: string | null
  selector?: string[] | null
  priority: number
  autonomy: Autonomy
  budget_usd: number
  max_turns: number
  timeout_sec: number
  max_attempts: number
  project_id?: string | null
}

export interface Schedule extends Base {
  name: string
  cron: string
  enabled: boolean
  template: TaskTemplate
  next_run_at: ISODate | null
  last_run_at: ISODate | null
  created_by: string
}

export type ForgeKind = 'gitea' | 'github' | 'gitlab'
export type IntegrationKind = ForgeKind | 'miabi' | 'posta'
export type ForgeAuth = 'token' | 'github_app'

export interface Integration extends Base {
  name: string
  kind: IntegrationKind
  base_url: string
  web_url: string
  auth_type: ForgeAuth
  username: string
  app_id?: number
  installation_id?: number
  /** A token or private key is stored (secrets are write-only). */
  has_secret: boolean
  created_by: string
  /** Where the forge (issue events) or Miabi (deploy and container events) sends webhooks. */
  webhook_url: string
  /** Default Miabi workspace id, uid or handle (kind miabi); empty for an account-wide key. */
  workspace?: string
  /** From address of notification email (kind posta). */
  sender?: string
  /** Extra CA (PEM) trusted for a self-signed or private-CA Miabi. */
  ca_cert?: string
  /** Miabi: the integration tool calls use when they name none. */
  default?: boolean
  /** GitLab: personal, project or group, read on Test. */
  token_kind?: string
  /** GitLab: when the token stops working. */
  token_expires_at?: ISODate
}

/** A Miabi workspace an integration's key can see; agents use only the enabled ones. */
export interface MiabiWorkspace {
  id: string
  integration_id: string
  /** The handle agents and policies use. */
  name: string
  miabi_id: string
  display_name: string
  role: string
  /** False when the key is bound to another workspace. */
  accessible: boolean
  enabled: boolean
  /** A live event stream is open. */
  streaming: boolean
  last_event_at: ISODate | null
  last_error: string
  synced_at: ISODate | null
}

export interface Project extends Base {
  name: string
  slug: string
  description: string
  integration_id: string
  /** Copied from the integration at creation (older rows may be empty). */
  forge?: ForgeKind | ''
  owner: string
  repo: string
  default_branch: string
  web_url: string
  agent_id: string | null
  selector: string[] | null
  sandbox_image: string
  instructions: string
  trigger_label: string
  created_by: string
}

export interface ProjectTemplate {
  id: string
  name: string
  description: string
  sandbox_image: string
  goal: string
  instructions: string
}

export interface MaintenancePreset {
  id: string
  name: string
  description: string
  cron: string
  goal: string
}

export interface ProjectTemplates {
  templates: ProjectTemplate[] | null
  presets: MaintenancePreset[] | null
}

export interface ProjectCreated {
  project: Project
  task?: Task | null
}

export interface AuditLog {
  id: number
  organization_id: string
  created_at: ISODate
  actor_type: string
  actor_id: string
  action: string
  target_type: string
  target_id: string
  ip: string
  metadata: Record<string, unknown> | null
  prev_hash: string
  hash: string
}

export type TaskQuery = { status?: string; project_id?: string; has_pr?: boolean } & PageQuery
export type SessionQuery = { agent_id?: string; task_id?: string; project_id?: string; mode?: string } & PageQuery
export type ChangeQuery = { status?: string; agent_id?: string; session_id?: string; task_id?: string } & PageQuery

/** One page of a list. `next` is the page number to ask for next; null on the last page. */
export interface Page<T> {
  items: T[]
  next: number | null
  /** Rows matching the query, when the server reported it (only while a next page exists). */
  total: number | null
}

/** Paging query parameters: a 0-based page and a page size (server default 50, at most 200). */
export type PageQuery = {
  page?: number
  size?: number
}

export interface VerifyResult {
  valid: boolean
  checked: number
  broken_at?: number
  reason?: string
}

export interface Overview {
  agents: { total: number; online: number; pending: number }
  tasks: { queued: number; running: number; succeeded_24h: number; failed_24h: number }
  pending_approvals: number
  pending_questions: number
  spend_today_usd: number
  spend_month_usd: number
  tokens_today: number
  kill_switch: boolean
  version: string
}

export interface UsageRow {
  agent_id: string
  model: string
  calls: number
  input_tokens: number
  output_tokens: number
  cost_usd: number
}

export interface TestResult {
  ok: boolean
  reply?: string
  error?: string
  latency_ms: number
}

/** Either a session, or (mfa_required) a challenge to finish with /auth/login/2fa. */
export interface LoginResponse {
  user?: User
  token?: string
  expires_at: ISODate
  mfa_required?: boolean
  mfa_token?: string
}

export interface TwoFactorStatus {
  enabled: boolean
  recovery_codes_left: number
}

export interface TOTPSetup {
  secret: string
  otpauth_uri: string
  /** PNG data URL of otpauth_uri. */
  qr_code: string
}

export interface MeResponse {
  user: User
  organization: Organization
  auth_method: string
}

/** Public: which sign-in methods the login page offers. */
export interface AuthProviders {
  password: boolean
  password_note?: string
  sso: boolean
  sso_name?: string
  /** Full-page navigation target; it redirects to the identity provider. */
  sso_login_url?: string
}

export type KMSProvider = 'local' | 'vault-transit'
export type AgentMTLS = 'off' | 'optional' | 'required'

export interface DataKey {
  id: string
  provider: string
  active: boolean
}

export interface SIEMSink {
  sink: string
  /** Last audit id delivered to this sink. */
  cursor: number
  /** Audit events not yet delivered. */
  lag: number
  last_sent_at?: ISODate | null
  last_error?: string
  error_at?: ISODate | null
}

export interface SecurityStatus {
  kms: KMSProvider | string
  data_keys: DataKey[] | null
  siem: SIEMSink[] | null
  sso: boolean
  agent_mtls: AgentMTLS | string
  tls: boolean
}

export interface APIKeyCreated {
  key: APIKey
  secret: string
}

export interface MessageResponse {
  message: string
}

/** A live event from the bus (both SSE streams). */
export interface BusEvent<T = unknown> {
  type: string
  session_id?: string
  agent_id?: string
  task_id?: string
  data?: T
  ts: ISODate
}

// Payloads of specific events.
export interface ToolRequestPayload {
  request_id: string
  tool_use_id: string
  tool: string
  input: unknown
  effect: Effect
  reason: string
  risk: Risk | 'unknown'
  approval_id: string
  /** Set when the call runs as part of an approved change plan. */
  change_id?: string
  phase?: ChangePhase | ''
}

export interface ToolResultPayload {
  request_id: string
  tool_use_id: string
  tool: string
  output: string
  is_error: boolean
  duration_ms: number
  truncated?: boolean
  change_id?: string
  phase?: ChangePhase | ''
}

/** What the UI shows as a tool's output: a tool.result event or a tool_result block. */
export interface ToolOutcome {
  output: string
  is_error: boolean
  duration_ms?: number
  truncated?: boolean
}

export interface DonePayload {
  outcome: string
  summary?: string
  error?: string
}

export interface StatusPayload {
  state: SessionState
  detail?: string
}

export interface UsagePayload {
  input_tokens: number
  output_tokens: number
  cost_usd: number
}

// ---- operations -----------------------------------------------------------------------------------

export type ChangeStatus = 'pending' | 'approved' | 'denied' | 'running' | 'succeeded' | 'failed' | 'rolled_back' | 'expired'
export type ChangePhase = 'step' | 'verify' | 'rollback'
export type ChangeCallStatus = 'pending' | 'running' | 'ok' | 'failed' | 'skipped'

export const CHANGE_STATUSES: ChangeStatus[] = ['pending', 'approved', 'running', 'succeeded', 'rolled_back', 'failed', 'denied', 'expired']

/** One call of a proposed plan (the change_run input, proto.ChangeCall). */
export interface PlanCall {
  tool: string
  input: unknown
  description?: string
  /** Verify: text the output must contain. */
  expect?: string
  /** Verify: text the output must not contain. */
  reject?: string
}

/** The input of a change_run call (proto.ChangePlan). */
export interface ChangePlan {
  title: string
  reason: string
  steps: PlanCall[]
  verify: PlanCall[]
  rollback?: PlanCall[] | null
}

/** A recorded call of a change with its outcome (models.ChangeCall). */
export interface ChangeCall extends PlanCall {
  phase: ChangePhase
  hash: string
  risk: Risk | string
  status: ChangeCallStatus
  output?: string
  duration_ms?: number
}

export interface Change extends Base {
  session_id: string
  task_id: string | null
  agent_id: string
  approval_id: string
  title: string
  reason: string
  risk: Risk | string
  status: ChangeStatus
  detail: string
  calls: ChangeCall[] | null
  started_at: ISODate | null
  finished_at: ISODate | null
}

export interface AlertRoute extends Base {
  name: string
  token_prefix: string
  enabled: boolean
  agent_id: string | null
  selector: string[] | null
  host_label: string
  match: Record<string, string> | null
  autonomy: Autonomy
  instructions: string
  last_alert_at: ISODate | null
  created_by: string
}

export interface AlertRouteInput {
  name: string
  enabled?: boolean
  agent_id?: string | null
  selector?: string[]
  host_label?: string
  match?: Record<string, string>
  autonomy?: Autonomy
  instructions?: string
}

/** Create and rotate return the webhook URL (with its secret token) exactly once. */
export interface AlertRouteCreated {
  route: AlertRoute
  webhook_url: string
}

/** A watched Miabi app: successful deploys get verified, failures get triaged. */
export interface MiabiWatch extends Base {
  integration_id: string
  /** Workspace handle; empty when the integration has a single workspace. */
  workspace: string
  /** App name or glob (*, api-*). */
  app: string
  app_id: number
  agent_id: string | null
  selector: string[] | null
  verify_deploys: boolean
  triage_failures: boolean
  /** Triage failed backups, restores, provisioning and upgrades. */
  databases: boolean
  health_url: string
  autonomy: Autonomy
  instructions: string
  last_event_at: ISODate | null
  created_by: string
}

export interface MiabiWatchInput {
  integration_id: string
  /** Required when the integration has several enabled workspaces. */
  workspace?: string
  /** Miabi app name, id or uid, or a glob (*, api-*); the server resolves it. */
  app: string
  agent_id?: string | null
  selector?: string[]
  verify_deploys?: boolean
  triage_failures?: boolean
  databases?: boolean
  health_url?: string
  autonomy?: Autonomy
  instructions?: string
}

export interface TerminalSession extends Base {
  agent_id: string
  user_id: string
  status: 'open' | 'closed'
  cols: number
  rows: number
  ended_at: ISODate | null
  exit_code: number
  bytes: number
  truncated: boolean
}

export type MCPTransport = 'stdio' | 'http'

/** An MCP server the control plane runs or reaches; its tools become mcp__<name>__<tool>. */
export interface MCPServer {
  id: string
  name: string
  transport: MCPTransport
  command: string
  args: string[] | null
  url: string
  /** Names only: values are write-only. */
  env_keys: string[] | null
  integration_id: string | null
  allow_write: boolean
  enabled: boolean
  last_error: string
  synced_at: ISODate | null
  created_at: ISODate
}

export interface MCPServerInput {
  name: string
  transport?: MCPTransport
  command?: string
  args?: string[]
  url?: string
  /** Omit or null to keep the stored values on update. */
  env?: Record<string, string> | null
  integration_id?: string | null
  allow_write?: boolean
  enabled?: boolean
}

export interface MCPTool {
  id: string
  server_id: string
  name: string
  description: string
  input_schema: JSONSchema | null
  read_only: boolean
  destructive: boolean
  enabled: boolean
  risk: '' | Risk
}

/** Create returns the server and its tools; error is set when listing the tools failed. */
export interface MCPServerCreated {
  server: MCPServer
  tools: MCPTool[] | null
  error?: string
}

// ---- chat channels and lessons ---------------------------------------------------------------------

export type ChatKind = 'slack' | 'telegram' | 'signal'

/** A chat bot connection (Slack, Telegram, Signal). Credentials are write-only. */
export interface ChatChannel {
  id: string
  name: string
  kind: ChatKind
  enabled: boolean
  api_base_url: string
  /** Signal: the bot's phone number. */
  account: string
  default_agent_id: string | null
  last_error: string
  last_seen_at: ISODate | null
  has_token: boolean
  has_secret: boolean
  /** Slack only: where Slack sends events and interactions. */
  events_url?: string
  interact_url?: string
  created_at: ISODate
  updated_at: ISODate
}

export interface ChatChannelInput {
  name: string
  kind: ChatKind
  enabled?: boolean
  api_base_url?: string
  account?: string
  /** Empty keeps the stored value on update. */
  token?: string
  signing_secret?: string
  default_agent_id?: string | null
}

/** A chat account linked to an Akili user. */
export interface ChatIdentity {
  id: string
  channel_id: string
  external_id: string
  display_name: string
  user_id: string
  last_used_at: ISODate | null
  created_at: ISODate
}

export interface LinkCode {
  code: string
  expires_at: ISODate
}

export type LessonStatus = 'proposed' | 'approved' | 'rejected'

/** A short fact an agent proposed (lesson_propose); only approved ones reach the system prompt. */
export interface Lesson {
  id: string
  /** null: applies to every agent. */
  agent_id: string | null
  text: string
  status: LessonStatus
  session_id: string | null
  task_id: string | null
  proposed_by: string
  decided_by: string | null
  decided_at: ISODate | null
  note: string
  created_at: ISODate
}

// ---- request bodies ------------------------------------------------------------------------------

export interface AgentInput {
  name?: string
  description?: string
  labels?: string[]
  policy_id?: string
  autonomy?: Autonomy
  provider_id?: string
  max_parallel?: number
  instructions?: string
  monthly_budget_usd?: number
  git_name?: string
  git_email?: string
  skill_ids?: string[]
}

export interface TaskInput {
  title: string
  goal: string
  agent_id?: string | null
  selector?: string[] | null
  priority: number
  autonomy: Autonomy
  budget_usd: number
  max_turns: number
  timeout_sec: number
  max_attempts: number
  project_id?: string | null
  /** Project plans the task works on (project tasks only). */
  plan_ids?: string[]
  /** Focus the task on one phase of a plan (that plan is linked too). */
  plan_phase_id?: string
  /** Save without queueing; startTask queues it. */
  draft?: boolean
}

export type PlanStatus = 'draft' | 'active' | 'in_progress' | 'done' | 'archived'
export type PhaseStatus = 'todo' | 'in_progress' | 'done' | 'skipped'

/** Work on a project written as phases (not a change plan: see ChangePlan). */
export interface ProjectPlan extends Base {
  project_id: string
  title: string
  description: string
  status: PlanStatus
  position: number
  /** A user id, or "agent:<id>" for a plan an agent proposed with plan_propose. */
  created_by: string
  /** The session an agent proposed the plan in. */
  proposed_session_id?: string
}

export interface PlanPhase extends Base {
  plan_id: string
  position: number
  title: string
  detail: string
  /** What finished means for this phase. */
  done_when: string
  status: PhaseStatus
  note: string
  /** The task whose agent last changed the phase; null when a person did. */
  done_by_task: string | null
  /** A user id, or "agent:<id>". */
  updated_by: string
}

export interface PlanSummary extends ProjectPlan {
  phases: number
  counts: Partial<Record<PhaseStatus, number>>
  tasks: number
}

export interface PlanDetail {
  plan: ProjectPlan
  phases: PlanPhase[] | null
  tasks: Task[] | null
  /** Which tasks work on the plan, and on which phase when focused on one. */
  links: { task_id: string; phase_id: string | null }[] | null
}

export interface PlanPhaseInput {
  id?: string
  title: string
  detail?: string
  done_when?: string
}

/** A plan as a task's agent received it. */
export interface TaskPlan {
  task_id: string
  plan_id: string
  /** The phase the task focuses on, or null for the whole plan. */
  phase_id: string | null
  snapshot: { title: string; description: string; phases: { id: string; title: string; detail?: string; done_when?: string; status: PhaseStatus }[] | null }
  created_at: ISODate
}

export interface ScheduleInput {
  name: string
  cron: string
  enabled: boolean
  template: TaskTemplate
}

export interface PolicyInput {
  name: string
  description: string
  document: PolicyDocument
}

export interface ProviderAgent {
  id: string
  name: string
  status: string
  /** Uses this provider as the organization default (no provider of its own). */
  via_default: boolean
}

export interface ProviderDetail extends ModelProvider {
  agents: ProviderAgent[]
  usage_30d: { calls: number; input_tokens: number; output_tokens: number; cost_usd: number }
}

export interface ProviderInput {
  name: string
  kind: ProviderKind
  base_url: string
  model: string
  effort: Effort
  max_tokens: number
  /** Context window in tokens; 0 = 200,000. */
  context_tokens: number
  api_key: string
  is_default: boolean
  input_price_mtok: number
  output_price_mtok: number
}

export interface NotificationSettings {
  email_approvals: boolean
  email_tasks: boolean
  /** False until an admin adds a default Posta integration. */
  email_available: boolean
  can_approve: boolean
}

export interface GitIdentitySettings {
  /** Mentioned in pull requests the user's sessions open; empty leaves it out. */
  forge_login: string
  /** Credited with a Co-Authored-By trailer on commits; empty leaves it out. */
  co_author_email: string
}

export interface IntegrationInput {
  name: string
  kind: IntegrationKind
  base_url: string
  web_url: string
  auth_type: ForgeAuth
  username: string
  /** Empty keeps the stored secret on update. */
  token: string
  app_id: number
  installation_id: number
  private_key: string
  webhook_secret: string
  workspace: string
  /** Miabi: CA certificate (PEM); empty removes it. */
  ca_cert: string
  sender?: string
}

export interface ProjectInput {
  name: string
  description: string
  integration_id?: string
  owner?: string
  repo?: string
  create_repo?: boolean
  private?: boolean
  template?: string
  agent_id: string | null
  selector: string[]
  sandbox_image: string
  instructions: string
  trigger_label: string
  autonomy?: Autonomy
}

export interface PresetInput {
  preset: string
  cron?: string
  autonomy?: Autonomy
  enabled?: boolean
}

export interface UserInput {
  email?: string
  name?: string
  role?: Role
  password?: string
  active?: boolean
}

// ---- transport -------------------------------------------------------------------------------------

export const API_BASE = '/api/v1'

export class ApiError extends Error {
  status: number
  code: string
  constructor(status: number, code: string, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}

export interface RequestOptions {
  /** Do not show a toast on failure (the caller handles it). */
  quiet?: boolean
  /** Do not redirect to /login on 401. */
  noAuthRedirect?: boolean
  signal?: AbortSignal
  /** Return the whole JSON envelope instead of its data (for paged lists). */
  envelope?: boolean
}

type Hooks = {
  onUnauthorized: () => void
  onError: (err: ApiError) => void
}

const hooks: Hooks = { onUnauthorized: () => {}, onError: () => {} }

/** Wires navigation and toasts without importing the router or stores here. */
export function configureApi(h: Partial<Hooks>): void {
  Object.assign(hooks, h)
}

async function request<T>(method: string, path: string, body?: unknown, opts: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  let res: Response
  try {
    res = await fetch(API_BASE + path, {
      method,
      headers,
      credentials: 'same-origin',
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: opts.signal,
    })
  } catch (e) {
    if (e instanceof DOMException && e.name === 'AbortError') throw e
    const err = new ApiError(0, 'NETWORK', 'Cannot reach the control plane. Check your connection.')
    if (!opts.quiet) hooks.onError(err)
    throw err
  }
  return parse<T>(res, opts)
}

/** Unwraps the JSON envelope, raising ApiError on failure. */
async function parse<T>(res: Response, opts: RequestOptions): Promise<T> {
  let payload: unknown = null
  const text = await res.text()
  if (text) {
    try {
      payload = JSON.parse(text)
    } catch {
      payload = null
    }
  }
  if (!res.ok) {
    const env = payload as { error?: { status_code?: number; code?: string; message?: string } } | null
    const err = new ApiError(res.status, env?.error?.code ?? 'ERROR', env?.error?.message || res.statusText || `HTTP ${res.status}`)
    if (res.status === 401 && !opts.noAuthRedirect) {
      hooks.onUnauthorized()
    } else if (!opts.quiet) {
      hooks.onError(err)
    }
    throw err
  }
  if (opts.envelope) return payload as T
  const env = payload as { success?: boolean; data?: T } | null
  return (env && 'data' in env ? env.data : (payload as T)) as T
}

/** POST a raw binary body (an image upload). */
async function upload<T>(path: string, body: Blob, opts: RequestOptions = {}): Promise<T> {
  let res: Response
  try {
    res = await fetch(API_BASE + path, {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/octet-stream' },
      credentials: 'same-origin',
      body,
      signal: opts.signal,
    })
  } catch (e) {
    if (e instanceof DOMException && e.name === 'AbortError') throw e
    const err = new ApiError(0, 'NETWORK', 'Cannot reach the control plane. Check your connection.')
    if (!opts.quiet) hooks.onError(err)
    throw err
  }
  return parse<T>(res, opts)
}

/** GET a plain-text body (e.g. a diff); errors use the usual JSON envelope. */
async function getText(path: string, opts: RequestOptions = {}): Promise<string> {
  let res: Response
  try {
    res = await fetch(API_BASE + path, { headers: { Accept: 'text/plain, text/x-diff, application/json' }, credentials: 'same-origin', signal: opts.signal })
  } catch (e) {
    if (e instanceof DOMException && e.name === 'AbortError') throw e
    const err = new ApiError(0, 'NETWORK', 'Cannot reach the control plane. Check your connection.')
    if (!opts.quiet) hooks.onError(err)
    throw err
  }
  const text = await res.text()
  if (!res.ok) {
    let msg = res.statusText || `HTTP ${res.status}`
    let code = 'ERROR'
    try {
      const env = JSON.parse(text) as { error?: { code?: string; message?: string } }
      msg = env?.error?.message || msg
      code = env?.error?.code ?? code
    } catch {
      /* not JSON */
    }
    const err = new ApiError(res.status, code, msg)
    if (res.status === 401 && !opts.noAuthRedirect) hooks.onUnauthorized()
    else if (!opts.quiet) hooks.onError(err)
    throw err
  }
  return text
}

const get = <T>(p: string, o?: RequestOptions) => request<T>('GET', p, undefined, o)

/** GET one page of a list. The server sends `pageable` only while a next page exists. */
async function getPage<T>(p: string, o?: RequestOptions): Promise<Page<T>> {
  const env = await request<{ data?: T[] | null; pageable?: { next_page: number; total_elements: number } } | null>('GET', p, undefined, { ...o, envelope: true })
  const items = env?.data ?? []
  return env?.pageable ? { items, next: env.pageable.next_page, total: env.pageable.total_elements } : { items, next: null, total: null }
}
const post = <T>(p: string, b?: unknown, o?: RequestOptions) => request<T>('POST', p, b ?? {}, o)
const put = <T>(p: string, b: unknown, o?: RequestOptions) => request<T>('PUT', p, b, o)
const patch = <T>(p: string, b: unknown, o?: RequestOptions) => request<T>('PATCH', p, b, o)
const del = <T>(p: string, o?: RequestOptions) => request<T>('DELETE', p, undefined, o)

function qs(params: Record<string, string | number | boolean | undefined | null>): string {
  const u = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== null && v !== '') u.set(k, String(v))
  }
  const s = u.toString()
  return s ? `?${s}` : ''
}

const enc = encodeURIComponent

// ---- endpoints -------------------------------------------------------------------------------------

export const api = {
  // auth
  login: (email: string, password: string) =>
    post<LoginResponse>('/auth/login', { email, password }, { noAuthRedirect: true, quiet: true }),
  loginMFA: (mfa_token: string, code: string) =>
    post<LoginResponse>('/auth/login/2fa', { mfa_token, code }, { noAuthRedirect: true, quiet: true }),
  logout: () => post<MessageResponse>('/auth/logout', {}, { noAuthRedirect: true, quiet: true }),
  me: (o?: RequestOptions) => get<MeResponse>('/auth/me', o),
  authProviders: () => get<AuthProviders>('/auth/providers', { noAuthRedirect: true, quiet: true }),
  changePassword: (current_password: string, new_password: string) =>
    post<MessageResponse>('/auth/password', { current_password, new_password }),
  notifications: () => get<NotificationSettings>('/auth/notifications'),
  updateNotifications: (b: Partial<Pick<NotificationSettings, 'email_approvals' | 'email_tasks'>>) => put<NotificationSettings>('/auth/notifications', b),
  gitIdentity: () => get<GitIdentitySettings>('/auth/git-identity'),
  updateGitIdentity: (b: Partial<GitIdentitySettings>) => put<GitIdentitySettings>('/auth/git-identity', b),
  testNotification: () => post<MessageResponse>('/auth/notifications/test', {}),
  twoFactor: () => get<TwoFactorStatus>('/auth/2fa'),
  setupTwoFactor: (password: string) => post<TOTPSetup>('/auth/2fa/setup', { password }, { quiet: true }),
  enableTwoFactor: (code: string) => post<{ recovery_codes: string[] }>('/auth/2fa/enable', { code }, { quiet: true }),
  disableTwoFactor: (code: string) => post<MessageResponse>('/auth/2fa/disable', { code }, { quiet: true }),
  regenerateRecoveryCodes: (code: string) => post<{ recovery_codes: string[] }>('/auth/2fa/recovery-codes', { code }, { quiet: true }),

  // users & keys
  listUsers: () => get<User[] | null>('/users'),
  createUser: (b: UserInput) => post<User>('/users', b),
  updateUser: (id: string, b: UserInput) => patch<User>(`/users/${enc(id)}`, b),
  resetUserTwoFactor: (id: string) => del<MessageResponse>(`/users/${enc(id)}/2fa`),
  listAPIKeys: () => get<APIKey[] | null>('/api-keys'),
  createAPIKey: (b: { name: string; scopes: string[]; expires_in_days: number }) => post<APIKeyCreated>('/api-keys', b),
  revokeAPIKey: (id: string) => del<MessageResponse>(`/api-keys/${enc(id)}`),
  vscodeAuthorize: (b: { challenge: string; state: string; client: string; editor: string; window: string }) => post<{ redirect: string }>('/auth/vscode/authorize', b),

  // agents
  listAgents: (o?: RequestOptions) => get<Agent[] | null>('/agents', o),
  getAgent: (id: string) => get<Agent>(`/agents/${enc(id)}`),
  createAgent: (b: AgentInput) => post<Enrollment>('/agents', b),
  updateAgent: (id: string, b: AgentInput) => patch<Agent>(`/agents/${enc(id)}`, b),
  reenrollAgent: (id: string) => post<Enrollment>(`/agents/${enc(id)}/reenroll`),
  revokeAgent: (id: string) => post<MessageResponse>(`/agents/${enc(id)}/revoke`),
  drainAgent: (id: string, draining: boolean) => post<MessageResponse>(`/agents/${enc(id)}/drain`, { draining }),
  deleteAgent: (id: string) => del<MessageResponse>(`/agents/${enc(id)}`),

  // sessions
  listSessions: (p: SessionQuery = {}) => get<ChatSession[] | null>('/sessions' + qs(p)),
  pageSessions: (p: SessionQuery = {}, o?: RequestOptions) => getPage<ChatSession>('/sessions' + qs(p), o),
  createSession: (agent_id: string, title = '', project_id = '') =>
    post<ChatSession>('/sessions', project_id ? { agent_id, title, project_id } : { agent_id, title }),
  getSession: (id: string, o?: RequestOptions) => get<SessionDetail>(`/sessions/${enc(id)}`, o),
  postMessage: (id: string, text: string, attachments: string[] = [], o?: RequestOptions) =>
    post<MessageResponse>(`/sessions/${enc(id)}/messages`, { text, attachments }, o),
  uploadAttachment: (id: string, file: Blob, o?: RequestOptions) => upload<Attachment>(`/sessions/${enc(id)}/attachments`, file, o),
  attachmentUrl: (sessionId: string, attachmentId: string) => `${API_BASE}/sessions/${enc(sessionId)}/attachments/${enc(attachmentId)}`,
  interruptSession: (id: string) => post<MessageResponse>(`/sessions/${enc(id)}/interrupt`),
  closeSession: (id: string) => post<MessageResponse>(`/sessions/${enc(id)}/close`),
  eventsStreamURL: (session?: string | null) => `${API_BASE}/events/stream${session ? `?session=${enc(session)}` : ''}`,

  // approvals
  listApprovals: (p: { status?: string } & PageQuery = {}, o?: RequestOptions) => get<Approval[] | null>('/approvals' + qs(p), o),
  pageApprovals: (p: { status?: string } & PageQuery = {}, o?: RequestOptions) => getPage<Approval>('/approvals' + qs(p), o),
  pageQuestions: (p: { status?: QuestionStatus; task_id?: string; session_id?: string } & PageQuery = {}, o?: RequestOptions) =>
    getPage<Question>('/questions' + qs(p), o),
  answerQuestion: (id: string, a: { choice: number } | { text: string }) => post<Question>(`/questions/${enc(id)}/answer`, a),
  approve: (id: string, note = '') => post<Approval>(`/approvals/${enc(id)}/approve`, { note }),
  deny: (id: string, note = '') => post<Approval>(`/approvals/${enc(id)}/deny`, { note }),

  // tasks
  listTasks: (p: TaskQuery = {}, o?: RequestOptions) => get<Task[] | null>('/tasks' + qs(p), o),
  pageTasks: (p: TaskQuery = {}, o?: RequestOptions) => getPage<Task>('/tasks' + qs(p), o),
  createTask: (b: TaskInput) => post<Task>('/tasks', b),
  getTask: (id: string, o?: RequestOptions) => get<Task>(`/tasks/${enc(id)}`, o),
  cancelTask: (id: string) => post<Task>(`/tasks/${enc(id)}/cancel`),
  retryTask: (id: string) => post<Task>(`/tasks/${enc(id)}/retry`),
  continueTask: (id: string, maxTurns = 0) => post<Task>(`/tasks/${enc(id)}/continue`, { max_turns: maxTurns }),
  startTask: (id: string) => post<Task>(`/tasks/${enc(id)}/start`),
  taskPlans: (id: string, o?: RequestOptions) => get<TaskPlan[] | null>(`/tasks/${enc(id)}/plans`, o),
  listPlans: (projectId: string, o?: RequestOptions) => get<PlanSummary[] | null>(`/projects/${enc(projectId)}/plans`, o),
  createPlan: (projectId: string, b: { title: string; description: string; status?: 'draft' | 'active'; phases: PlanPhaseInput[] }) =>
    post<PlanDetail>(`/projects/${enc(projectId)}/plans`, b),
  getPlan: (id: string, o?: RequestOptions) => get<PlanDetail>(`/plans/${enc(id)}`, o),
  updatePlan: (id: string, b: { title?: string; description?: string; status?: 'draft' | 'active' | 'archived'; position?: number }) =>
    put<ProjectPlan>(`/plans/${enc(id)}`, b),
  deletePlan: (id: string) => del<MessageResponse>(`/plans/${enc(id)}`),
  replacePlanPhases: (id: string, phases: PlanPhaseInput[]) => put<PlanDetail>(`/plans/${enc(id)}/phases`, { phases }),
  updatePlanPhase: (planId: string, phaseId: string, b: { status: PhaseStatus; note?: string }) => patch<PlanPhase>(`/plans/${enc(planId)}/phases/${enc(phaseId)}`, b),
  taskDiff: (id: string, o?: RequestOptions) => getText(`/tasks/${enc(id)}/diff`, o),

  // integrations (admin): git forges and Miabi
  listIntegrations: (o?: RequestOptions) => get<Integration[] | null>('/integrations', o),
  createIntegration: (b: IntegrationInput) => post<Integration>('/integrations', b),
  updateIntegration: (id: string, b: IntegrationInput) => put<Integration>(`/integrations/${enc(id)}`, b),
  deleteIntegration: (id: string, o?: RequestOptions) => del<MessageResponse>(`/integrations/${enc(id)}`, o),
  testIntegration: (id: string) => post<TestResult>(`/integrations/${enc(id)}/test`),
  setDefaultIntegration: (id: string) => post<Integration>(`/integrations/${enc(id)}/default`, {}),
  listMiabiWorkspaces: (id: string, o?: RequestOptions) => get<MiabiWorkspace[] | null>(`/integrations/${enc(id)}/miabi-workspaces`, o),
  syncMiabiWorkspaces: (id: string, o?: RequestOptions) => post<MiabiWorkspace[] | null>(`/integrations/${enc(id)}/miabi-workspaces/sync`, {}, o),
  updateMiabiWorkspace: (id: string, wsId: string, enabled: boolean, o?: RequestOptions) =>
    put<MiabiWorkspace>(`/integrations/${enc(id)}/miabi-workspaces/${enc(wsId)}`, { enabled }, o),

  // projects
  listProjects: (o?: RequestOptions) => get<Project[] | null>('/projects', o),
  getProject: (id: string, o?: RequestOptions) => get<Project>(`/projects/${enc(id)}`, o),
  createProject: (b: ProjectInput, o?: RequestOptions) => post<ProjectCreated>('/projects', b, o),
  updateProject: (id: string, b: ProjectInput, o?: RequestOptions) => put<Project>(`/projects/${enc(id)}`, b, o),
  deleteProject: (id: string) => del<MessageResponse>(`/projects/${enc(id)}`),
  addMaintenance: (id: string, b: PresetInput) => post<Schedule>(`/projects/${enc(id)}/maintenance`, b),
  projectTemplates: (o?: RequestOptions) => get<ProjectTemplates>('/project-templates', o),

  // schedules
  listSchedules: () => get<Schedule[] | null>('/schedules'),
  createSchedule: (b: ScheduleInput) => post<Schedule>('/schedules', b),
  updateSchedule: (id: string, b: ScheduleInput) => put<Schedule>(`/schedules/${enc(id)}`, b),
  deleteSchedule: (id: string) => del<MessageResponse>(`/schedules/${enc(id)}`),
  runSchedule: (id: string) => post<Task>(`/schedules/${enc(id)}/run`),

  // skills
  listSkills: () => get<Skill[] | null>('/skills'),
  getSkill: (id: string) => get<Skill>(`/skills/${enc(id)}`),
  createSkill: (b: { name: string; description: string; content: string }) => post<Skill>('/skills', b),
  updateSkill: (id: string, b: { name: string; description: string; content: string }) => put<Skill>(`/skills/${enc(id)}`, b),
  deleteSkill: (id: string) => del<MessageResponse>(`/skills/${enc(id)}`),

  // policies
  listPolicies: () => get<Policy[] | null>('/policies'),
  getPolicy: (id: string) => get<Policy>(`/policies/${enc(id)}`),
  createPolicy: (b: PolicyInput) => post<Policy>('/policies', b),
  updatePolicy: (id: string, b: PolicyInput) => put<Policy>(`/policies/${enc(id)}`, b),
  deletePolicy: (id: string) => del<MessageResponse>(`/policies/${enc(id)}`),
  simulatePolicy: (id: string, b: { tool: string; input: unknown; autonomy: Autonomy; workdir?: string }) =>
    post<Decision>(`/policies/${enc(id)}/simulate`, b),
  listTools: (o?: RequestOptions) => get<ToolSpec[] | null>('/tools', o),

  // providers (admin)
  listProviders: (o?: RequestOptions) => get<ModelProvider[] | null>('/providers', o),
  getProvider: (id: string, o?: RequestOptions) => get<ProviderDetail>(`/providers/${enc(id)}`, o),
  createProvider: (b: ProviderInput) => post<ModelProvider>('/providers', b),
  updateProvider: (id: string, b: ProviderInput) => put<ModelProvider>(`/providers/${enc(id)}`, b),
  deleteProvider: (id: string) => del<MessageResponse>(`/providers/${enc(id)}`),
  testProvider: (id: string) => post<TestResult>(`/providers/${enc(id)}/test`),

  // operations: change plans, alert routes, terminals
  listChanges: (p: ChangeQuery = {}, o?: RequestOptions) => get<Change[] | null>('/changes' + qs(p), o),
  pageChanges: (p: ChangeQuery = {}, o?: RequestOptions) => getPage<Change>('/changes' + qs(p), o),
  getChange: (id: string, o?: RequestOptions) => get<Change>(`/changes/${enc(id)}`, o),
  listAlertRoutes: (o?: RequestOptions) => get<AlertRoute[] | null>('/alert-routes', o),
  createAlertRoute: (b: AlertRouteInput) => post<AlertRouteCreated>('/alert-routes', b),
  updateAlertRoute: (id: string, b: AlertRouteInput) => put<AlertRoute>(`/alert-routes/${enc(id)}`, b),
  rotateAlertRoute: (id: string) => post<AlertRouteCreated>(`/alert-routes/${enc(id)}/rotate`),
  deleteAlertRoute: (id: string) => del<MessageResponse>(`/alert-routes/${enc(id)}`),
  listMiabiWatches: (o?: RequestOptions) => get<MiabiWatch[] | null>('/miabi-watches', o),
  createMiabiWatch: (b: MiabiWatchInput, o?: RequestOptions) => post<MiabiWatch>('/miabi-watches', b, o),
  updateMiabiWatch: (id: string, b: MiabiWatchInput, o?: RequestOptions) => put<MiabiWatch>(`/miabi-watches/${enc(id)}`, b, o),
  deleteMiabiWatch: (id: string) => del<MessageResponse>(`/miabi-watches/${enc(id)}`),
  listTerminals: (p: { agent_id?: string } & PageQuery = {}, o?: RequestOptions) => get<TerminalSession[] | null>('/terminals' + qs(p), o),
  pageTerminals: (p: { agent_id?: string } & PageQuery = {}, o?: RequestOptions) => getPage<TerminalSession>('/terminals' + qs(p), o),
  getTerminal: (id: string, o?: RequestOptions) => get<TerminalSession>(`/terminals/${enc(id)}`, o),
  terminalRecording: (id: string, o?: RequestOptions) => getText(`/terminals/${enc(id)}/recording`, o),
  /** The terminal WebSocket, on this origin (ws: or wss: to match the page). */
  terminalURL: (agentId: string, cols: number, rows: number) => {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
    return `${proto}//${location.host}${API_BASE}/agents/${enc(agentId)}/terminal${qs({ cols, rows })}`
  },
  /**
   * Explains a terminal refusal: browsers hide a failed WebSocket handshake's status, so ask the same
   * URL over plain HTTP. The server checks the agent and its policy before upgrading (409 offline,
   * 403 not allowed) and answers 400 to a non-WebSocket request that passed, so nothing opens.
   */
  terminalPreflight: async (agentId: string): Promise<number> => {
    try {
      const r = await fetch(`${API_BASE}/agents/${enc(agentId)}/terminal`, { credentials: 'same-origin', headers: { Accept: 'application/json' } })
      return r.status
    } catch {
      return 0
    }
  },

  // MCP gateway (admin)
  listMCPServers: (o?: RequestOptions) => get<MCPServer[] | null>('/mcp-servers', o),
  createMCPServer: (b: MCPServerInput, o?: RequestOptions) => post<MCPServerCreated>('/mcp-servers', b, o),
  updateMCPServer: (id: string, b: MCPServerInput, o?: RequestOptions) => put<MCPServer>(`/mcp-servers/${enc(id)}`, b, o),
  deleteMCPServer: (id: string) => del<MessageResponse>(`/mcp-servers/${enc(id)}`),
  syncMCPServer: (id: string, o?: RequestOptions) => post<MCPTool[] | null>(`/mcp-servers/${enc(id)}/sync`, {}, o),
  listMCPTools: (id: string, o?: RequestOptions) => get<MCPTool[] | null>(`/mcp-servers/${enc(id)}/tools`, o),
  updateMCPTool: (id: string, toolId: string, b: { enabled: boolean; risk: '' | Risk }, o?: RequestOptions) =>
    put<MCPTool>(`/mcp-servers/${enc(id)}/tools/${enc(toolId)}`, b, o),

  // chat channels (admin writes) and linked chat accounts
  listChatChannels: (o?: RequestOptions) => get<ChatChannel[] | null>('/chat/channels', o),
  createChatChannel: (b: ChatChannelInput) => post<ChatChannel>('/chat/channels', b),
  updateChatChannel: (id: string, b: ChatChannelInput) => put<ChatChannel>(`/chat/channels/${enc(id)}`, b),
  deleteChatChannel: (id: string) => del<MessageResponse>(`/chat/channels/${enc(id)}`),
  testChatChannel: (id: string) => post<TestResult>(`/chat/channels/${enc(id)}/test`),
  chatLinkCode: () => post<LinkCode>('/chat/link-code'),
  listChatIdentities: (o?: RequestOptions) => get<ChatIdentity[] | null>('/chat/identities', o),
  deleteChatIdentity: (id: string) => del<MessageResponse>(`/chat/identities/${enc(id)}`),

  // lessons
  pageLessons: (p: { status?: LessonStatus; agent_id?: string } & PageQuery = {}, o?: RequestOptions) => getPage<Lesson>('/lessons' + qs(p), o),
  createLesson: (b: { text: string; agent_id?: string }) => post<Lesson>('/lessons', b),
  approveLesson: (id: string, b: { text: string; note: string }) => post<Lesson>(`/lessons/${enc(id)}/approve`, b),
  rejectLesson: (id: string, note = '') => post<Lesson>(`/lessons/${enc(id)}/reject`, { note }),
  deleteLesson: (id: string) => del<MessageResponse>(`/lessons/${enc(id)}`),

  // system
  overview: (o?: RequestOptions) => get<Overview>('/overview', o),
  usage: (days = 30) => get<UsageRow[] | null>('/usage' + qs({ days })),
  pageAudit: (p: { action?: string; actor_id?: string; target_id?: string } & PageQuery, o?: RequestOptions) => getPage<AuditLog>('/audit' + qs(p), o),
  verifyAudit: () => get<VerifyResult>('/audit/verify'),
  security: (o?: RequestOptions) => get<SecurityStatus>('/security', o),
  getSandboxSettings: () => get<{ root: boolean }>('/system/sandbox'),
  setSandboxSettings: (b: { root: boolean }) => put<{ root: boolean }>('/system/sandbox', b),
  setKillSwitch: (enabled: boolean) => post<{ enabled: boolean }>('/system/kill-switch', { enabled }),
  getLicense: (o?: RequestOptions) => get<LicenseInfo>('/license', o),
  installLicense: (token: string) => put<LicenseInfo>('/license', { token }),
  removeLicense: () => del<LicenseInfo>('/license'),
}
