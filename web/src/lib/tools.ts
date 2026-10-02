// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Shared knowledge about tools: their icons, the one-line summary of a call, and change plans.
import type { ChangeCall, ChangePlan, ChangePhase, PlanCall, ToolOutcome, ToolRequestPayload } from '../api'
import type { IconName } from '../components/Icon'

export const TOOL_ICONS: Record<string, IconName> = {
  shell: 'terminal',
  fs_read: 'file',
  fs_list: 'folder',
  fs_write: 'edit',
  fs_edit: 'edit',
  search: 'search',
  http_fetch: 'globe',
  host_info: 'server',
  git_status: 'gitBranch',
  git_diff: 'gitDiff',
  git_commit: 'gitCommit',
  git_push: 'push',
  sandbox_exec: 'box',
  pr_open: 'gitPR',
  pr_status: 'gitPR',
  // operator (host) tools
  service_status: 'settings',
  service_restart: 'refresh',
  journal_logs: 'scroll',
  disk_usage: 'hardDrive',
  process_list: 'cpu',
  docker_ps: 'box',
  docker_logs: 'scroll',
  docker_restart: 'refresh',
  cert_check: 'award',
  package_updates: 'package',
  net_probe: 'network',
  change_run: 'clipboard',
  miabi_apps: 'layers',
  miabi_status: 'activity',
  miabi_deployments: 'list',
  miabi_releases: 'tag',
  miabi_logs: 'scroll',
  miabi_deploy_logs: 'scroll',
  miabi_deploy: 'push',
  miabi_rollback: 'undo',
  miabi_restart: 'refresh',
  miabi_workspaces: 'folder',
  miabi_overview: 'overview',
  miabi_alerts: 'siren',
  miabi_alert: 'bell',
  miabi_events: 'activity',
  miabi_databases: 'hardDrive',
  miabi_db_backups: 'list',
  miabi_db_backup: 'package',
  miabi_db_restore: 'undo',
  miabi_traffic: 'gauge',
  miabi_env: 'key',
  miabi_env_set: 'key',
  miabi_scale: 'layers',
  miabi_maintenance: 'pause',
  miabi_canary: 'flask',
  miabi_stack_restart: 'refresh',
  miabi_cronjobs: 'schedules',
  miabi_cron_run: 'play',
  miabi_pipelines: 'gitBranch',
  miabi_pipeline_run: 'play',
  lesson_propose: 'lightbulb',
}

/** Splits an MCP gateway tool name (mcp__<server>__<tool>); null for any other tool. */
export function mcpTool(name: string | undefined): { server: string; tool: string } | null {
  const m = /^mcp__([a-z0-9-]+)__(.+)$/.exec(name ?? '')
  return m ? { server: m[1], tool: m[2] } : null
}

export function toolIcon(name: string | undefined): IconName {
  if (mcpTool(name)) return 'plug'
  return (name && TOOL_ICONS[name]) || 'wrench'
}

/** The group a tool is listed under: "Miabi" for the miabi_* tools, "MCP · <server>" for MCP tools. */
export function toolGroup(name: string | undefined): string {
  const mcp = mcpTool(name)
  if (mcp) return `MCP · ${mcp.server}`
  return name?.startsWith('miabi_') ? 'Miabi' : 'Agent & forge'
}

// The argument that says what a call touches, in order of preference.
const KEY_ARGS = ['command', 'unit', 'container', 'path', 'url', 'host', 'pattern', 'query', 'message', 'title', 'grep', 'since']

const scalar = (v: unknown): string => (typeof v === 'string' ? v : typeof v === 'number' && v ? String(v) : '')

// Tools that act on a named workspace resource rather than an app; the prefix matches the policy pattern.
const MIABI_NAMED: Record<string, string> = { miabi_stack_restart: 'stack', miabi_cron_run: 'cron', miabi_pipeline_run: 'pipeline' }

// Miabi calls name workspace/app (or a database, stack, cron job or pipeline), plus what changes.
// miabi_env_set never shows its value: it may be a secret.
function miabiSummary(i: Record<string, unknown>, tool?: string): string {
  const ws = scalar(i.workspace)
  const at = (what: string) => (ws ? `${ws}/${what}` : what)
  const database = scalar(i.database)
  if (database) {
    const db = scalar(i.db)
    const backup = scalar(i.backup)
    return `${at('db')} ${database}${db ? `/${db}` : ''}${backup ? ` backup ${backup}` : ''}`
  }
  const kind = tool ? MIABI_NAMED[tool] : undefined
  const name = scalar(i.name)
  if (kind && name) {
    const branch = scalar(i.branch)
    return `${at(kind)} ${name}${branch ? ` @ ${branch}` : ''}`
  }
  const alert = scalar(i.alert)
  if (alert) return `${ws ? `${ws} · ` : ''}alert ${alert}${scalar(i.action) ? ` → ${scalar(i.action)}` : ''}`
  const app = scalar(i.app)
  if (!app) return ws
  const target = at(app)
  const tag = scalar(i.tag)
  const release = scalar(i.release_id)
  const deployment = scalar(i.deployment_id)
  if (tag) return `${target} → ${tag}`
  if (release) return `${target} → release ${release}`
  if (deployment) return `${target} · deployment ${deployment}`
  if (typeof i.replicas === 'number') return `${target} ×${i.replicas} replica${i.replicas === 1 ? '' : 's'}`
  if (tool === 'miabi_maintenance' && typeof i.enabled === 'boolean') return `${target} maintenance ${i.enabled ? 'on' : 'off'}`
  if (tool === 'miabi_canary' && scalar(i.action)) return `${target} canary ${scalar(i.action)}`
  if (tool === 'miabi_env_set' && scalar(i.key)) return `${target} set ${scalar(i.key)}`
  if (tool === 'miabi_traffic' && scalar(i.range)) return `${target} · ${scalar(i.range)}`
  return target
}

// MCP arguments have no known shape: show the first one or two plain values.
function mcpSummary(i: Record<string, unknown>): string {
  const vals = Object.values(i)
    .map((v) => (typeof v === 'string' ? v : typeof v === 'number' || typeof v === 'boolean' ? String(v) : ''))
    .filter(Boolean)
    .slice(0, 2)
  return truncate(vals.join(' · '), 80)
}

/** The text cut to max characters (whitespace collapsed), with an ellipsis when cut. */
export function truncate(text: string, max: number): string {
  const t = text.replace(/\s+/g, ' ').trim()
  return t.length > max ? t.slice(0, max - 1).trimEnd() + '…' : t
}

/** A short "what" for a call: its most telling argument (e.g. the command, the unit or the app). */
export function callSummary(input: unknown, tool?: string): string {
  const i = input as Record<string, unknown> | null
  if (!i || typeof i !== 'object' || Array.isArray(i)) return ''
  if (tool === 'lesson_propose') return truncate(scalar(i.lesson), 80)
  if (mcpTool(tool)) return mcpSummary(i)
  if (tool?.startsWith('miabi_') || (!tool && typeof i.app === 'string')) {
    const m = miabiSummary(i, tool)
    if (m) return m
  }
  for (const k of KEY_ARGS) {
    const v = i[k]
    if (typeof v === 'string' && v) return k === 'host' && typeof i.port === 'number' && i.port ? `${v}:${i.port}` : v
  }
  return ''
}

export interface MiabiState {
  status: string
  health: string
  /** ok | degraded | no-data */
  traffic: string
  /** on | off */
  maintenance: string
}

/** The app state a Miabi tool reports ("status: running", "health: unhealthy", "traffic: ok", "maintenance: on"), when it does. */
export function miabiState(output: string | undefined): MiabiState | null {
  if (!output) return null
  const line = (key: string) => (new RegExp(`^\\s*${key}:\\s*(\\S+)`, 'im').exec(output)?.[1] ?? '').toLowerCase()
  const st: MiabiState = { status: line('status'), health: line('health'), traffic: line('traffic'), maintenance: line('maintenance') }
  return st.status || st.health || st.traffic || st.maintenance ? st : null
}

/** Every argument as "key=value", for a compact but complete view of a call. */
export function callArgs(input: unknown): { key: string; value: string }[] {
  const i = input as Record<string, unknown> | null
  if (!i || typeof i !== 'object' || Array.isArray(i)) return []
  return Object.entries(i)
    .filter(([, v]) => v !== undefined && v !== null && v !== '')
    .map(([key, v]) => ({ key, value: typeof v === 'string' ? v : JSON.stringify(v) }))
}

function asCalls(v: unknown): PlanCall[] {
  if (!Array.isArray(v)) return []
  return v
    .filter((c): c is Record<string, unknown> => !!c && typeof c === 'object' && typeof (c as { tool?: unknown }).tool === 'string')
    .map((c) => ({
      tool: String(c.tool),
      input: c.input ?? {},
      description: typeof c.description === 'string' ? c.description : '',
      expect: typeof c.expect === 'string' ? c.expect : '',
      reject: typeof c.reject === 'string' ? c.reject : '',
    }))
}

/** Reads a change_run input (object or JSON text); null when it is not a plan. */
export function parsePlan(input: unknown): ChangePlan | null {
  let v = input
  if (typeof v === 'string') {
    try {
      v = JSON.parse(v)
    } catch {
      return null
    }
  }
  if (!v || typeof v !== 'object' || Array.isArray(v)) return null
  const o = v as Record<string, unknown>
  if (!Array.isArray(o.steps)) return null
  return {
    title: typeof o.title === 'string' ? o.title : '',
    reason: typeof o.reason === 'string' ? o.reason : '',
    steps: asCalls(o.steps),
    verify: asCalls(o.verify),
    rollback: asCalls(o.rollback),
  }
}

export const PHASES: { key: ChangePhase; title: string; icon: IconName; help: string }[] = [
  { key: 'step', title: 'Steps', icon: 'play', help: 'Run in order; the first failure stops the change.' },
  { key: 'verify', title: 'Verify', icon: 'flask', help: 'Read-only checks after the steps; any failure triggers the rollback.' },
  { key: 'rollback', title: 'Rollback', icon: 'undo', help: 'Runs only if a step or check failed.' },
]

export function phaseLabel(phase: string | undefined): string {
  return phase === 'verify' ? 'check' : phase === 'rollback' ? 'rollback' : 'step'
}

/**
 * The outcome of a verify call. The agent fails a check whose output misses `expect` or contains
 * `reject`, but the control plane records the call itself as ok; re-derive the check from the output.
 */
export function checkFailed(c: ChangeCall): string {
  if (c.phase !== 'verify' || c.status !== 'ok') return ''
  const out = c.output ?? ''
  const truncated = out.endsWith('…')
  if (c.expect && !out.includes(c.expect) && !truncated) return `output did not contain “${c.expect}”`
  if (c.reject && out.includes(c.reject)) return `output contained “${c.reject}”`
  return ''
}

/** The status a change call should show: its recorded status, or check_failed. */
export function callStatus(c: ChangeCall): string {
  return checkFailed(c) ? 'check_failed' : c.status
}

/** A call the agent ran as part of a change plan, shown inside the change_run card. */
export interface ChangeChild {
  key: string
  name: string
  input: unknown
  request?: ToolRequestPayload
  result?: ToolOutcome
  tag: string
}
