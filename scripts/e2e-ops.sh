#!/usr/bin/env bash
# Copyright (c) 2026 Jonas Kaninda.
# SPDX-License-Identifier: Apache-2.0
# End-to-end test of operations: alert, triage, approved change plan, verify or rollback, and a recorded terminal.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
AGENT_DIR="${AGENT_DIR:-$ROOT/agent}"
WORK="$(mktemp -d)"
PG_PORT=${PG_PORT:-55492}
REDIS_PORT=${REDIS_PORT:-56439}
PORT=${PORT:-18480}
BASE="http://127.0.0.1:$PORT"
API="$BASE/api/v1"
JAR="$WORK/cookies"
PASS="e2e-password-123456"
SERVER_PID=""; AGENT_PID=""; REVIEWER_PID=""

cleanup() {
  [ -n "$AGENT_PID" ] && kill "$AGENT_PID" 2>/dev/null || true
  [ -n "$REVIEWER_PID" ] && kill "$REVIEWER_PID" 2>/dev/null || true
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
[ "$(api GET '/audit?action=change.succeeded' | json "(d.get('pageable') or {}).get('total_elements', len(d['data']))")" -ge 1 ] || fail "change not audited"

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

task() { api POST /tasks "$(python3 -c 'import json,sys; print(json.dumps({"goal": sys.argv[1], "agent_id": sys.argv[2], "autonomy": 2}))' "$1" "$AGENT")" | json "d['data']['id']"; }
tstatus() { api GET "/tasks/$1" | json "d['data']['status']"; }
finished() { case "$(tstatus "$1")" in succeeded|failed|cancelled|timed_out) return 0;; *) return 1;; esac; }
approval_for() { api GET '/approvals?status=pending' | json "[a['id'] for a in d['data'] if a['task_id']=='$1'][0]"; }
code() {
  if [ $# -ge 3 ]; then curl -s -o "$WORK/code.out" -w '%{http_code}' -b "$JAR" -X "$1" -H 'Content-Type: application/json' -d "$3" "$API$2"
  else curl -s -o "$WORK/code.out" -w '%{http_code}' -b "$JAR" -X "$1" "$API$2"; fi
}

step "Reviewer agents: a second agent reviews a change plan read-only; its review is advice, not a decision"
created=$(api POST /agents "{\"name\":\"reviewer-1\",\"policy_id\":\"$POLICY\",\"autonomy\":2}")
REVIEWER=$(echo "$created" | json "d['data']['agent']['id']")
mkdir -p "$WORK/st2" "$WORK/wk2"
AKILI_JOIN_TOKEN=$(echo "$created" | json "d['data']['join_token']") AKILI_LOG_FORMAT=text "$WORK/akili-agent" enroll --url "$BASE" --state-dir "$WORK/st2" --workdir "$WORK/wk2" >>"$WORK/reviewer.log" 2>&1
AKILI_LOG_FORMAT=text "$WORK/akili-agent" start --state-dir "$WORK/st2" >>"$WORK/reviewer.log" 2>&1 &
REVIEWER_PID=$!
rv_online() { [ "$(api GET /agents/$REVIEWER | json "d['data']['status']")" = "online" ]; }
wait_for "reviewer online" 30 rv_online
RPLAN=$(python3 -c 'import json; print("change: "+json.dumps({"title":"Write rev","reason":"review test","steps":[{"tool":"fs_write","input":{"path":"cache/rev.txt","content":"x"}}],"verify":[{"tool":"fs_list","input":{"path":"cache"},"expect":"rev.txt"}]}))')
TR=$(task "$RPLAN")
rv_change() { api GET '/changes?status=pending' | json "[c['id'] for c in d['data'] if c['task_id']=='$TR'][0]"; }
wait_for "change plan to review" 30 rv_change
CHR=$(rv_change)
[ "$(code POST "/changes/$CHR/review" "{\"agent_id\":\"$AGENT\"}")" = "400" ] || fail "an agent was allowed to review its own plan"
RV=$(api POST "/changes/$CHR/review" "{\"agent_id\":\"$REVIEWER\"}" | json "d['data']['id']")
rv_done() { [ "$(api GET "/reviews?target_type=change&target_id=$CHR" | json "[r['status'] for r in d['data'] if r['id']=='$RV'][0]")" = "done" ]; }
wait_for "review" 60 rv_done
[ "$(api GET "/reviews?target_id=$CHR" | json "d['data'][0]['verdict']")" = "concerns" ] || fail "review verdict: $(api GET "/reviews?target_id=$CHR")"
RVT=$(api GET "/reviews?target_id=$CHR" | json "d['data'][0]['reviewer_task_id']")
RVS=$(api GET "/tasks/$RVT" | json "d['data']['session_id']")
api GET "/sessions/$RVS" | json "[e['payload'].get('reason','') for e in d['data']['events'] if e['type']=='tool.request' and e['payload'].get('tool')=='fs_write'][0]" | grep >/dev/null "reviewers are read-only" || fail "the reviewer's write was not refused as read-only"
[ ! -f "$WORK/wk2/reviewer-was-here.txt" ] || fail "the reviewer wrote a file"
[ "$(api GET "/changes/$CHR" | json "d['data']['status']")" = "pending" ] || fail "a review decided the change"
api POST "/approvals/$(api GET "/changes/$CHR" | json "d['data']['approval_id']")/deny" '{}' >/dev/null

step "Incident memory: an approved postmortem is recalled when the same alert fires again"
python3 - "$WORK/route-mem.json" <<'PY'
import json, sys
pm = {"lesson": "CacheFull came from stale build files in cache/; deleting cache/*.bin fixed it and fs_list confirmed the space.", "kind": "postmortem", "alert": "CacheFull"}
json.dump({"name": "memory", "match": {"team": "mem"}, "instructions": "tool: lesson_propose " + json.dumps(pm)}, open(sys.argv[1], "w"))
PY
HOOKM=$(api_file POST /alert-routes "$WORK/route-mem.json" | json "d['data']['webhook_url']")
TM1=$(curl -sS -H 'Content-Type: application/json' -d '{"title":"CacheFull","host":"ops-1","labels":{"team":"mem"}}' "$HOOKM" | json "d['data']['created'][0]")
wait_for "first CacheFull triage" 60 finished "$TM1"
pm_id() { api GET '/lessons?status=proposed' | json "[l['id'] for l in d['data'] if l.get('kind')=='postmortem' and l.get('alert')=='CacheFull'][0]"; }
wait_for "proposed postmortem" 30 pm_id
PM=$(pm_id)
api POST "/lessons/$PM/approve" '{}' >/dev/null
TM2=$(curl -sS -H 'Content-Type: application/json' -d '{"title":"CacheFull","host":"ops-1","labels":{"team":"mem","seen":"again"}}' "$HOOKM" | json "d['data']['created'][0]")
recalled() { api GET "/sessions/$(api GET "/tasks/$TM2" | json "d['data']['session_id']")" | json "[e['payload'] for e in d['data']['events'] if e['type']=='lessons.recalled'][0]['lesson_ids']" | grep -q "$PM"; }
wait_for "the postmortem in the next triage" 60 recalled

step "Grants: approve once and allow similar calls; denials and API keys never grant"
TG1=$(task 'tool: fs_write {"path":"cache/granted.txt","content":"one"}')
wait_for "fs_write approval" 30 approval_for "$TG1"
APG=$(approval_for "$TG1")
api POST "/approvals/$APG/approve" '{}' >/dev/null
GRANT_BODY="{\"agent_id\":\"$AGENT\",\"tool\":\"fs_write\",\"match\":{\"path\":{\"equals\":\"cache/granted.txt\"},\"content\":{\"glob\":\"*\"}},\"max_risk\":\"medium\",\"expires_in_hours\":1,\"max_uses\":1,\"reason\":\"e2e: routine writes\",\"approval_id\":\"$APG\"}"
KEY=$(api POST /api-keys '{"name":"e2e","scopes":["read","write"],"expires_in_days":1}' | json "d['data']['secret']")
[ "$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $KEY" -H 'Content-Type: application/json' -d "$GRANT_BODY" "$API/grants")" = "403" ] || fail "an API key created a grant"
[ "$(api GET /grants | json "len(d['data'])")" = "0" ] || fail "a refused API-key request still stored a grant"
[ "$(code POST /grants "$(echo "$GRANT_BODY" | sed 's/"max_risk":"medium"/"max_risk":"critical"/')")" = "400" ] && grep -q critical "$WORK/code.out" || fail "a critical grant was accepted: $(cat "$WORK/code.out")"
GRANT=$(api POST /grants "$GRANT_BODY" | json "d['data']['id']")
wait_for "first write" 30 finished "$TG1"
TG2=$(task 'tool: fs_write {"path":"cache/granted.txt","content":"two"}')
wait_for "granted write" 30 finished "$TG2"
[ "$(tstatus "$TG2")" = "succeeded" ] || fail "granted task: $(tstatus "$TG2")"
[ "$(cat "$WORK/wk/cache/granted.txt")" = "two" ] || fail "the granted call did not run"
[ "$(api GET "/grants?agent_id=$AGENT" | json "[g['uses'] for g in d['data'] if g['id']=='$GRANT'][0]")" = "1" ] || fail "grant use not counted"
[ "$(api GET '/audit?action=tool.request&size=200' | json "len([e for e in d['data'] if (e.get('metadata') or {}).get('grant_id')=='$GRANT'])")" -ge 1 ] || fail "granted call not audited with its grant"
TG3=$(task 'tool: fs_write {"path":"cache/granted.txt","content":"three"}')
wait_for "approval once the grant is used up" 30 approval_for "$TG3"
api POST "/approvals/$(approval_for "$TG3")/deny" '{}' >/dev/null
TG3B=$(task 'tool: fs_write {"path":"cache/other.txt","content":"x"}')
wait_for "approval outside the grant" 30 approval_for "$TG3B"
api POST "/approvals/$(approval_for "$TG3B")/deny" '{}' >/dev/null

step "Freeze: grants pause and the call needs a person again"
GRANT2=$(api POST /grants "$(echo "$GRANT_BODY" | sed 's/"max_uses":1/"max_uses":0/')" | json "d['data']['id']")
FROM=$(python3 -c 'import datetime as d; print((d.datetime.now(d.timezone.utc)-d.timedelta(hours=1)).isoformat())')
TO=$(python3 -c 'import datetime as d; print((d.datetime.now(d.timezone.utc)+d.timedelta(hours=1)).isoformat())')
FREEZE=$(api POST /change-windows "{\"name\":\"launch\",\"kind\":\"freeze\",\"starts_at\":\"$FROM\",\"ends_at\":\"$TO\",\"enabled\":true}" | json "d['data']['id']")
[ "$(api GET /change-windows | json "[w['active'] for w in d['data'] if w['id']=='$FREEZE'][0]")" = "True" ] || fail "freeze not active"
TG4=$(task 'tool: fs_write {"path":"cache/granted.txt","content":"frozen"}')
wait_for "approval during the freeze" 30 approval_for "$TG4"
api POST "/approvals/$(approval_for "$TG4")/deny" '{}' >/dev/null
api DELETE "/change-windows/$FREEZE" >/dev/null
api POST "/grants/$GRANT2/revoke" >/dev/null

step "Runbooks: a granted plan within bounds runs without a person; outside them it asks"
python3 - "$WORK/runbook.json" <<'PY'
import json, sys
plan = {"steps": [{"tool": "fs_write", "input": {"path": "cache/rb.conf", "content": "v={{v}}"}}],
        "verify": [{"tool": "fs_read", "input": {"path": "cache/rb.conf"}, "expect": "v="}], "rollback": []}
json.dump({"name": "set rb", "plan": plan, "params": [{"name": "v", "bounds": {"one_of": ["1", "2"]}}], "enabled": True}, open(sys.argv[1], "w"))
PY
RB=$(api_file POST /runbooks "$WORK/runbook.json" | json "d['data']['id']")
BARE="{\"agent_id\":\"$AGENT\",\"tool\":\"change_run\",\"max_risk\":\"high\",\"expires_in_hours\":1,\"reason\":\"x\"}"
[ "$(code POST /grants "$BARE")" = "400" ] && grep -q runbook "$WORK/code.out" || fail "a bare change_run grant was accepted: $(cat "$WORK/code.out")"
api POST /grants "{\"agent_id\":\"$AGENT\",\"tool\":\"change_run\",\"runbook_id\":\"$RB\",\"max_risk\":\"high\",\"expires_in_hours\":1,\"reason\":\"e2e runbook\"}" >/dev/null
rb_plan() { python3 -c 'import json,sys; print("change: "+json.dumps({"title":"Set rb","reason":"e2e","steps":[{"tool":"fs_write","input":{"path":"cache/rb.conf","content":"v="+sys.argv[1]}}],"verify":[{"tool":"fs_read","input":{"path":"cache/rb.conf"},"expect":"v="}],"rollback":[]}))' "$1"; }
TG5=$(task "$(rb_plan 2)")
wait_for "granted runbook task" 60 finished "$TG5"
[ "$(cat "$WORK/wk/cache/rb.conf")" = "v=2" ] || fail "the granted runbook did not run"
[ "$(api GET '/changes?status=succeeded' | json "len([c for c in d['data'] if c['task_id']=='$TG5'])")" = "1" ] || fail "the granted change is not recorded"
TG6=$(task "$(rb_plan 3)")
wait_for "approval outside the runbook bounds" 30 approval_for "$TG6"
api POST "/approvals/$(approval_for "$TG6")/deny" '{}' >/dev/null
[ "$(cat "$WORK/wk/cache/rb.conf")" = "v=2" ] || fail "a plan outside the bounds ran"
[ "$(code GET "/agents/$AGENT/autonomy")" = "200" ] || fail "agent autonomy: $(cat "$WORK/code.out")"

step "Stop conditions: a task repeating a failing call is paused for an operator"
api PUT /system/autonomy '{"stop_repeat_failures":2}' >/dev/null
TG7=$(task "$(printf 'read: cache/missing.txt\nread: cache/missing.txt\nread: cache/missing.txt')")
stop_q() { api GET "/questions?status=pending&task_id=$TG7" | json "[q['id'] for q in d['data'] if q.get('kind')=='stop'][0]"; }
wait_for "stop question" 30 stop_q
api POST "/questions/$(stop_q)/answer" '{"choice":1}' >/dev/null
wait_for "paused task ends" 30 finished "$TG7"
[ "$(api GET '/audit?action=task.paused' | json "len(d['data'])")" -ge 1 ] || fail "pause not audited"
api PUT /system/autonomy '{}' >/dev/null

step "Kill switch revokes every grant"
api POST /grants "$(echo "$GRANT_BODY" | sed 's/"max_uses":1/"max_uses":0/')" >/dev/null
api POST /system/kill-switch '{"enabled":true}' >/dev/null
[ "$(api GET '/grants?active=true' | json "len(d['data'])")" = "0" ] || fail "grants survived the kill switch"
api POST /system/kill-switch '{"enabled":false}' >/dev/null

step "Recorded terminal (admin, policy allows it)"
cat > "$WORK/term.mjs" <<'JS'
const [,, url, cookie, origin] = process.argv
const ws = new WebSocket(url, { headers: { Cookie: cookie, Origin: origin } })
ws.binaryType = 'arraybuffer'
let out = '', sent = false, opened = false
const timer = setTimeout(() => { console.log('TIMEOUT ' + JSON.stringify(out)); process.exit(1) }, 20000)
ws.onopen = () => { opened = true; setTimeout(() => { ws.send(JSON.stringify({ type: 'input', data: 'echo TERM_OK_$((40+2))\n' })); sent = true }, 800) }
ws.onmessage = (ev) => {
  if (typeof ev.data === 'string') { const m = JSON.parse(ev.data); if (m.type === 'exit') { clearTimeout(timer); console.log(out.includes('TERM_OK_42') ? 'OK' : 'NOOUTPUT ' + JSON.stringify(out)); process.exit(0) } return }
  out += new TextDecoder().decode(ev.data)
  if (sent && out.includes('TERM_OK_42')) ws.send(JSON.stringify({ type: 'input', data: 'exit\n' }))
}
// HANDSHAKE: the upgrade was refused before the terminal opened (the server log has the status).
ws.onerror = (e) => { console.log((opened ? 'ERROR ' : 'HANDSHAKE ') + (e.message || e.error?.message || e.type)); process.exit(1) }
ws.onclose = (e) => { clearTimeout(timer); console.log('CLOSED ' + e.code + ' ' + e.reason + ' ' + JSON.stringify(out)); process.exit(1) }
JS
COOKIE="akili_session=$(awk '$6=="akili_session"{print $7}' "$JAR")"
# The terminal handler refuses an agent whose presence lapsed, so wait until the API says online.
online() { [ "$(api GET /agents/$AGENT | json "d['data']['status']")" = "online" ]; }
wait_for "agent online before the terminal" 30 online
# || true: under set -e a failing client would end the script before fail can say why.
term() { node "$WORK/term.mjs" "ws://127.0.0.1:$PORT/api/v1/agents/$AGENT/terminal?cols=100&rows=30" "$COOKIE" "$BASE" 2>&1 || true; }
RES=$(term)
case "$RES" in HANDSHAKE*)
  echo "terminal handshake refused once ($RES); retrying"; sleep 2; RES=$(term) ;;
esac
[ "$RES" = "OK" ] || fail "terminal: $RES"
recorded() { [ "$(api GET "/terminals?agent_id=$AGENT" | json "d['data'][0]['status']")" = "closed" ]; }
wait_for "terminal recording saved" 15 recorded
TRM=$(api GET "/terminals?agent_id=$AGENT" | json "d['data'][0]['id']")
curl -sS -b "$JAR" "$API/terminals/$TRM/recording" > "$WORK/cast"
head -1 "$WORK/cast" | grep >/dev/null '"version":2' || fail "recording is not an asciinema v2 cast"
grep -q 'TERM_OK_42' "$WORK/cast" || fail "recording lacks the output"
grep -q '"i","echo TERM_OK' "$WORK/cast" || fail "recording lacks the operator input"
[ "$(api GET '/audit?action=terminal.open' | json "(d.get('pageable') or {}).get('total_elements', len(d['data']))")" -ge 1 ] || fail "terminal.open not audited"

step "Terminal refused when the policy does not allow it, and for cross-site origins"
[ "$(curl -s -o /dev/null -w '%{http_code}' -b "$JAR" -H 'Connection: Upgrade' -H 'Upgrade: websocket' -H 'Sec-WebSocket-Version: 13' -H 'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==' -H 'Origin: https://evil.example' "$API/agents/$AGENT/terminal")" = "403" ] || fail "cross-site origin accepted"
DEV=$(api GET /policies | json "[p['id'] for p in d['data'] if p['name']=='developer'][0]")
api PATCH "/agents/$AGENT" "{\"policy_id\":\"$DEV\"}" >/dev/null
[ "$(curl -s -o /dev/null -w '%{http_code}' -b "$JAR" "$API/agents/$AGENT/terminal")" = "403" ] || fail "terminal allowed without the policy"

step "Audit chain verifies"
[ "$(api GET /audit/verify | json "d['data']['valid']")" = "True" ] || fail "audit chain invalid"

printf '\n\033[32mE2E OPS PASSED\033[0m\n'
