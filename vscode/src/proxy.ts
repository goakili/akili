// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// The sidebar webview asks the extension to make API calls for it (web/src/vscode/bridge.ts). Only
// the calls the sidebar needs are forwarded, so script injected into the webview can't use the
// user's key for anything else (users, policies, agents' terminals, API keys, integrations).

const ID = '[A-Za-z0-9_-]{1,64}'
const QUERY = '(\\?[^#\\s]*)?'

const rules: { method: string; path: RegExp }[] = [
  ['GET', `/auth/me`],
  ['GET', `/overview`],
  ['GET', `/agents${QUERY}`],
  ['GET', `/tools${QUERY}`],
  ['GET', `/policies`],
  ['GET', `/skills`],
  ['GET', `/projects/${ID}`],
  ['GET', `/sessions${QUERY}`],
  ['GET', `/sessions/${ID}`],
  ['GET', `/sessions/${ID}/attachments/${ID}`],
  ['POST', `/sessions`],
  ['POST', `/sessions/${ID}/messages`],
  ['POST', `/sessions/${ID}/attachments`],
  ['POST', `/sessions/${ID}/interrupt`],
  ['POST', `/sessions/${ID}/close`],
  ['GET', `/changes${QUERY}`],
  ['GET', `/changes/${ID}`],
  ['GET', `/approvals${QUERY}`],
  ['POST', `/approvals/${ID}/approve`],
  ['POST', `/approvals/${ID}/deny`],
  ['GET', `/tasks${QUERY}`],
  ['GET', `/tasks/${ID}`],
  ['GET', `/tasks/${ID}/diff`],
  ['POST', `/tasks`],
  ['POST', `/tasks/${ID}/cancel`],
  ['POST', `/tasks/${ID}/retry`],
].map(([method, p]) => ({ method, path: new RegExp(`^/api/v1${p}$`) }))

const streamPath = new RegExp(`^/api/v1/events/stream(\\?session=${ID})?$`)

/** Whether the webview may make this call through the extension. */
export function allowedCall(method: string, path: string): boolean {
  if (path.includes('..') || path.includes('//') || path.includes('%2e') || path.includes('%2E') || path.includes('%2f') || path.includes('%2F')) {
    return false
  }
  return rules.some((r) => r.method === method && r.path.test(path))
}

/** Whether the webview may open this live stream. */
export function allowedStream(path: string): boolean {
  return streamPath.test(path)
}
