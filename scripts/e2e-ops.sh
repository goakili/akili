#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Jonas Kaninda
# SPDX-License-Identifier: AGPL-3.0-or-later
# End-to-end test of operations: an Alertmanager disk-full alert becomes a triage task on
# the right host, the agent proposes a change plan, a human approves it, the agent executes and
# verifies it; a failing check rolls back; alerts are deduplicated; a recorded terminal works and
# is refused without the policy; runbook skills are seeded read-only. Scripted provider, Docker.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# The agent is its own repository, checked out next to this one.
AGENT_DIR="${AGENT_DIR:-$ROOT/agent}"
WORK="$(mktemp -d)"
PG_PORT=${PG_PORT:-55492}
REDIS_PORT=${REDIS_PORT:-56439}
PORT=${PORT:-18480}
BASE="http://127.0.0.1:$PORT"
API="$BASE/api/v1"
JAR="$WORK/cookies"
PASS="e2e-password-123456"
SERVER_PID=""; AGENT_PID=""

cleanup() {
  [ -n "$AGENT_PID" ] && kill "$AGENT_PID" 2>/dev/null || true
  [ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null || true
  docker rm -f akili-o-pg akili-o-redis >/dev/null 2>&1 || true
  if [ "${KEEP:-}" = "1" ]; then echo "logs kept in $WORK"; else rm -rf "$WORK"; fi
}
trap cleanup EXIT

step() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }
fail() { printf '\033[31mFAIL: %s\033[0m\n' "$*"; echo "--- server log"; grep -v "Incoming request" "$WORK/server.log" | tail -30 || true; echo "--- agent log"; tail -30 "$WORK/agent.log" || true; KEEP=1; exit 1; }
json() { python3 -c "import json,sys; d=json.load(sys.stdin); print(eval(sys.argv[1], {'d': d}))" "$1"; }
api() {
  local m=$1 p=$2 b=${3:-}
  if [ -n "$b" ]; then curl -sS -b "$JAR" -c "$JAR" -X "$m" -H 'Content-Type: application/json' -d "$b" "$API$p"
  else curl -sS -b "$JAR" -c "$JAR" -X "$m" "$API$p"; fi
}
api_file() { curl -sS -b "$JAR" -X "$1" -H 'Content-Type: application/json' --data-binary "@$3" "$API$2"; }
wait_for() {
  local desc=$1 t=$2; shift 2
  for _ in $(seq "$t"); do if "$@" >/dev/null 2>&1; then return 0; fi; sleep 1; done
  fail "timed out waiting for $desc"
}

step "Starting Postgres, Redis and the control plane"
docker rm -f akili-o-pg akili-o-redis >/dev/null 2>&1 || true
docker run -d --name akili-o-pg -e POSTGRES_USER=akili -e POSTGRES_PASSWORD=akili -e POSTGRES_DB=akili -p "$PG_PORT:5432" postgres:17-alpine >/dev/null
docker run -d --name akili-o-redis -p "$REDIS_PORT:6379" redis:7-alpine >/dev/null
wait_for "postgres" 60 docker exec akili-o-pg pg_isready -U akili
(cd "$ROOT/server" && go build -o "$WORK/akili" ./cmd/akili)
(cd "$AGENT_DIR" && go build -o "$WORK/akili-agent" ./cmd/akili-agent)
AKILI_ENV=development AKILI_PORT=$PORT AKILI_PUBLIC_URL=$BASE AKILI_ENV_FILE=/dev/null \
AKILI_DATABASE_URL="postgres://akili:akili@127.0.0.1:$PG_PORT/akili?sslmode=disable" \
AKILI_REDIS_ADDR="127.0.0.1:$REDIS_PORT" AKILI_ADMIN_EMAIL=admin@e2e.local AKILI_ADMIN_PASSWORD=$PASS \
ANTHROPIC_API_KEY= "$WORK/akili" server >"$WORK/server.log" 2>&1 &
SERVER_PID=$!
wait_for "control plane" 60 curl -fsS "$BASE/healthz"
api POST /auth/login "{\"email\":\"admin@e2e.local\",\"password\":\"$PASS\"}" >/dev/null

step "Runbook skills are seeded and read-only"
[ "$(api GET /skills | json "len([s for s in d['data'] if s['builtin']])")" -ge 7 ] || fail "runbooks not seeded"
SKILL=$(api GET /skills | json "[s['id'] for s in d['data'] if s['name']=='Disk space'][0]")
[ "$(curl -s -o /dev/null -w '%{http_code}' -b "$JAR" -X PUT -H 'Content-Type: application/json' -d '{"name":"x","content":"y"}' "$API/skills/$SKILL")" = "403" ] || fail "built-in runbook was editable"

step "Agent ops-1 with the operator policy (L2) and a nearly full cache"
POLICY=$(api GET /policies | json "[p['id'] for p in d['data'] if p['name']=='operator'][0]")
created=$(api POST /agents "{\"name\":\"ops-1\",\"policy_id\":\"$POLICY\",\"autonomy\":2,\"skill_ids\":[\"$SKILL\"]}")
AGENT=$(echo "$created" | json "d['data']['agent']['id']")
TOKEN=$(echo "$created" | json "d['data']['join_token']")
mkdir -p "$WORK/st" "$WORK/wk/cache"
dd if=/dev/zero of="$WORK/wk/cache/big.bin" bs=1048576 count=20 2>/dev/null
echo keep > "$WORK/wk/cache/keep.txt"
AKILI_JOIN_TOKEN=$TOKEN AKILI_LOG_FORMAT=text "$WORK/akili-agent" enroll --url "$BASE" --state-dir "$WORK/st" --workdir "$WORK/wk" >>"$WORK/agent.log" 2>&1
AKILI_LOG_FORMAT=text "$WORK/akili-agent" start --state-dir "$WORK/st" >>"$WORK/agent.log" 2>&1 &
AGENT_PID=$!
online() { [ "$(api GET /agents/$AGENT | json "d['data']['status']")" = "online" ]; }
wait_for "agent online" 30 online

step "Alert route for critical alerts (its instructions script the triage)"
python3 - "$WORK/route.json" <<'PY'
import json, sys
instructions = "\n".join([
    'tool: disk_usage {"path":"cache"}',
    'change: ' + json.dumps({"title": "Free the cache", "reason": "disk full alert: cache/big.bin is 20MB of stale data",
        "steps": [{"tool": "shell", "input": {"command": "rm -f cache/big.bin"}, "description": "delete the stale cache file"}],
        "verify": [{"tool": "fs_list", "input": {"path": "cache"}, "expect": "keep.txt", "reject": "big.bin"}],
        "rollback": []}),
])
json.dump({"name": "critical", "match": {"severity": "critical"}, "instructions": instructions}, open(sys.argv[1], "w"))
PY
ROUTE=$(api_file POST /alert-routes "$WORK/route.json")
HOOK=$(echo "$ROUTE" | json "d['data']['webhook_url']")
api GET /alert-routes | grep >/dev/null "${HOOK##*/}" && fail "alert route token was returned by the list API"

step "Alertmanager fires DiskFull on ops-1: a triage task lands on that agent, duplicates are skipped"
AM='{"version":"4","status":"firing","alerts":[{"status":"firing","labels":{"alertname":"DiskFull","instance":"ops-1:9100","severity":"critical"},"annotations":{"summary":"Disk 97% full"},"fingerprint":"fp-disk-1"},{"status":"resolved","labels":{"alertname":"Old","instance":"ops-1:9100","severity":"critical"},"fingerprint":"fp-old"}]}'
R1=$(curl -sS -H 'Content-Type: application/json' -d "$AM" "$HOOK")
T1=$(echo "$R1" | json "d['data']['created'][0]")
[ "$(echo "$R1" | json "len(d['data']['created'])")" = "1" ] || fail "expected exactly one task (resolved alert must be skipped): $R1"
[ "$(curl -sS -H 'Content-Type: application/json' -d "$AM" "$HOOK" | json "len(d['data']['created'])")" = "0" ] || fail "duplicate alert created a second task"
[ "$(curl -s -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' -d "$AM" "$BASE/api/v1/webhooks/alerts/akr_wrong")" = "404" ] || fail "unknown alert route token accepted"
[ "$(api GET /tasks/$T1 | json "d['data']['agent_id']")" = "$AGENT" ] || fail "task not targeted at the alerting host"
[ "$(api GET /tasks/$T1 | json "d['data']['trigger']")" = "alert" ] || fail "task trigger"

step "The agent investigates, then proposes a change plan that waits for a human"
pending() { [ "$(api GET '/changes?status=pending' | json "len(d['data'])")" -ge 1 ]; }
wait_for "pending change plan" 60 pending
CHG=$(api GET '/changes?status=pending' | json "d['data'][0]['id']")
[ "$(api GET /changes/$CHG | json "len(d['data']['calls'])")" = "2" ] || fail "change calls"
[ -f "$WORK/wk/cache/big.bin" ] || fail "something ran before approval"
APR=$(api GET "/changes/$CHG" | json "d['data']['approval_id']")
api POST "/approvals/$APR/approve" '{"note":"go ahead"}' >/dev/null

step "Approved: the agent executes the step, verifies it, and reports"
done_change() { [ "$(api GET /changes/$CHG | json "d['data']['status']")" = "succeeded" ]; }
wait_for "change succeeded" 60 done_change
[ ! -f "$WORK/wk/cache/big.bin" ] || fail "the cleanup did not happen"
[ -f "$WORK/wk/cache/keep.txt" ] || fail "the cleanup removed too much"
[ "$(api GET /changes/$CHG | json "[c['status'] for c in d['data']['calls']]")" = "['ok', 'ok']" ] || fail "call outcomes: $(api GET /changes/$CHG)"
task_done() { [ "$(api GET /tasks/$T1 | json "d['data']['status']")" = "succeeded" ]; }
wait_for "triage task finished" 60 task_done
[ "$(api GET '/audit?action=change.succeeded' | json "d['data']['total']")" -ge 1 ] || fail "change not audited"

step "A failing check rolls the change back automatically"
python3 - "$WORK/route2.json" <<'PY'
import json, sys
plan = {"title": "Bump config", "reason": "test rollback",
        "steps": [{"tool": "fs_write", "input": {"path": "cache/app.conf", "content": "v=2"}}],
        "verify": [{"tool": "fs_read", "input": {"path": "cache/app.conf"}, "expect": "v=3"}],
        "rollback": [{"tool": "fs_write", "input": {"path": "cache/app.conf", "content": "v=1"}}]}
json.dump({"name": "rollback-test", "match": {"team": "rb"}, "instructions": "change: " + json.dumps(plan)}, open(sys.argv[1], "w"))
PY
HOOK2=$(api_file POST /alert-routes "$WORK/route2.json" | json "d['data']['webhook_url']")
curl -sS -H 'Content-Type: application/json' -d '{"title":"Config drift","host":"ops-1","labels":{"team":"rb"}}' "$HOOK2" >/dev/null
pending2() { [ "$(api GET '/changes?status=pending' | json "len(d['data'])")" -ge 1 ]; }
wait_for "second change plan" 60 pending2
CHG2=$(api GET '/changes?status=pending' | json "d['data'][0]['id']")
api POST "/approvals/$(api GET /changes/$CHG2 | json "d['data']['approval_id']")/approve" '{}' >/dev/null
rolled() { [ "$(api GET /changes/$CHG2 | json "d['data']['status']")" = "rolled_back" ]; }
wait_for "rolled back" 60 rolled
[ "$(cat "$WORK/wk/cache/app.conf")" = "v=1" ] || fail "rollback did not restore the file"
[ "$(api GET /changes/$CHG2 | json "[c['status'] for c in d['data']['calls']]")" = "['ok', 'failed', 'ok']" ] || fail "failed check not recorded as failed: $(api GET /changes/$CHG2 | json "[c['status'] for c in d['data']['calls']]")"
[ "$(api GET '/approvals' | json "[a.get('change_id') for a in d['data'] if a['tool']=='change_run'][0]")" != "None" ] || fail "approval does not link its change"

step "Recorded terminal (admin, policy allows it)"
cat > "$WORK/term.mjs" <<'JS'
const [,, url, cookie, origin] = process.argv
const ws = new WebSocket(url, { headers: { Cookie: cookie, Origin: origin } })
ws.binaryType = 'arraybuffer'
let out = '', sent = false
const timer = setTimeout(() => { console.log('TIMEOUT ' + JSON.stringify(out)); process.exit(1) }, 20000)
ws.onopen = () => setTimeout(() => { ws.send(JSON.stringify({ type: 'input', data: 'echo TERM_OK_$((40+2))\n' })); sent = true }, 800)
ws.onmessage = (ev) => {
  if (typeof ev.data === 'string') { const m = JSON.parse(ev.data); if (m.type === 'exit') { clearTimeout(timer); console.log(out.includes('TERM_OK_42') ? 'OK' : 'NOOUTPUT ' + JSON.stringify(out)); process.exit(0) } return }
  out += new TextDecoder().decode(ev.data)
  if (sent && out.includes('TERM_OK_42')) ws.send(JSON.stringify({ type: 'input', data: 'exit\n' }))
}
ws.onerror = (e) => { console.log('ERROR ' + (e.message || e.type)); process.exit(1) }
JS
COOKIE="akili_session=$(awk '$6=="akili_session"{print $7}' "$JAR")"
RES=$(node "$WORK/term.mjs" "ws://127.0.0.1:$PORT/api/v1/agents/$AGENT/terminal?cols=100&rows=30" "$COOKIE" "$BASE")
[ "$RES" = "OK" ] || fail "terminal: $RES"
recorded() { [ "$(api GET "/terminals?agent_id=$AGENT" | json "d['data'][0]['status']")" = "closed" ]; }
wait_for "terminal recording saved" 15 recorded
TRM=$(api GET "/terminals?agent_id=$AGENT" | json "d['data'][0]['id']")
curl -sS -b "$JAR" "$API/terminals/$TRM/recording" > "$WORK/cast"
head -1 "$WORK/cast" | grep >/dev/null '"version":2' || fail "recording is not an asciinema v2 cast"
grep -q 'TERM_OK_42' "$WORK/cast" || fail "recording lacks the output"
grep -q '"i","echo TERM_OK' "$WORK/cast" || fail "recording lacks the operator input"
[ "$(api GET '/audit?action=terminal.open' | json "d['data']['total']")" -ge 1 ] || fail "terminal.open not audited"

step "Terminal refused when the policy does not allow it, and for cross-site origins"
[ "$(curl -s -o /dev/null -w '%{http_code}' -b "$JAR" -H 'Connection: Upgrade' -H 'Upgrade: websocket' -H 'Sec-WebSocket-Version: 13' -H 'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==' -H 'Origin: https://evil.example' "$API/agents/$AGENT/terminal")" = "403" ] || fail "cross-site origin accepted"
DEV=$(api GET /policies | json "[p['id'] for p in d['data'] if p['name']=='developer'][0]")
api PATCH "/agents/$AGENT" "{\"policy_id\":\"$DEV\"}" >/dev/null
[ "$(curl -s -o /dev/null -w '%{http_code}' -b "$JAR" "$API/agents/$AGENT/terminal")" = "403" ] || fail "terminal allowed without the policy"

step "Audit chain verifies"
[ "$(api GET /audit/verify | json "d['data']['valid']")" = "True" ] || fail "audit chain invalid"

printf '\n\033[32mE2E OPS PASSED\033[0m\n'
