#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Jonas Kaninda
# SPDX-License-Identifier: AGPL-3.0-or-later
# End-to-end test of the Miabi integration against a fake Miabi API with two workspaces (staging, prod):
#  - one account-wide key; workspaces are discovered and enabled one by one
#  A) staging deploys an unhealthy release → the event STREAM (no webhook) → verification task →
#     rollback plan → approved → rolled back → verified healthy
#  - signed webhooks still work (prod deploy.failed → triage), duplicates are skipped
#  - database events (backup.failed) open triage tasks for a databases watch
#  B) the agent deploys prod with change_run; the check fails and the runtime rolls back
#  - policy "workspace/app" rules hold; workspaces are addressed by name only; disabled or
#    unreachable (key bound elsewhere) workspaces are refused
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# The agent is its own repository, checked out next to this one.
AGENT_DIR="${AGENT_DIR:-$ROOT/agent}"
WORK="$(mktemp -d)"
PG_PORT=${PG_PORT:-55522}
REDIS_PORT=${REDIS_PORT:-56469}
PORT=${PORT:-18680}
MIABI_PORT=${MIABI_PORT:-18681}
BASE="http://127.0.0.1:$PORT"
API="$BASE/api/v1"
MIABI="http://127.0.0.1:$MIABI_PORT"
MKEY="mb_e2e_0123456789abcdef"
HOOK_SECRET="miabi-hook-secret"
JAR="$WORK/cookies"
PASS="e2e-password-123456"
SERVER_PID=""; AGENT_PID=""; MIABI_PID=""

cleanup() {
  for p in "$AGENT_PID" "$SERVER_PID" "$MIABI_PID"; do [ -n "$p" ] && kill "$p" 2>/dev/null || true; done
  docker rm -f akili-m-pg akili-m-redis >/dev/null 2>&1 || true
  if [ "${KEEP:-}" = "1" ]; then echo "logs kept in $WORK"; else rm -rf "$WORK"; fi
}
trap cleanup EXIT

step() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }
fail() { [ -n "${HOLD:-}" ] && { echo "HOLDING"; sleep 400; }; printf '\033[31mFAIL: %s\033[0m\n' "$*"; echo "--- server log"; grep -v "Incoming request" "$WORK/server.log" | tail -25 || true; echo "--- agent log"; tail -20 "$WORK/agent.log" || true; KEEP=1; exit 1; }
json() { python3 -c "import json,sys; d=json.load(sys.stdin); print(eval(sys.argv[1], {'d': d}))" "$1"; }
api() {
  local m=$1 p=$2 b=${3:-}
  if [ -n "$b" ]; then curl -sS -b "$JAR" -c "$JAR" -X "$m" -H 'Content-Type: application/json' -d "$b" "$API$p"
  else curl -sS -b "$JAR" -c "$JAR" -X "$m" "$API$p"; fi
}
api_file() { curl -sS -b "$JAR" -X "$1" -H 'Content-Type: application/json' --data-binary "@$3" "$API$2"; }
code() { curl -sS -o "$WORK/code.out" -w '%{http_code}' -b "$JAR" -X "$1" -H 'Content-Type: application/json' -d "$3" "$API$2"; }
wait_for() {
  local desc=$1 t=$2; shift 2
  for _ in $(seq "$t"); do if "$@" >/dev/null 2>&1; then return 0; fi; sleep 1; done
  fail "timed out waiting for $desc"
}
active_tag() { curl -sS "$MIABI/_fake/state" | json "d['data']['${1:-staging}/api']['active']"; }
miabi_hook() { # body [signature-secret]
  local sig; sig=$(printf '%s' "$1" | openssl dgst -sha256 -hmac "${2:-$HOOK_SECRET}" | awk '{print $NF}')
  curl -sS -o "$WORK/hook.out" -w '%{http_code}' -H "X-Miabi-Signature: sha256=$sig" -H 'Content-Type: application/json' -d "$1" "$API/webhooks/miabi/$INT"
}

step "Starting Postgres, Redis, a fake Miabi and the control plane"
docker rm -f akili-m-pg akili-m-redis >/dev/null 2>&1 || true
docker run -d --name akili-m-pg -e POSTGRES_USER=akili -e POSTGRES_PASSWORD=akili -e POSTGRES_DB=akili -p "$PG_PORT:5432" postgres:17-alpine >/dev/null
docker run -d --name akili-m-redis -p "$REDIS_PORT:6379" redis:7-alpine >/dev/null
wait_for "postgres" 60 docker exec akili-m-pg pg_isready -U akili
(cd "$ROOT/server" && go build -o "$WORK/akili" ./cmd/akili && go build -o "$WORK/fakemiabi" ./cmd/fakemiabi)
# The real Miabi CLI, for its MCP server: MIABI_CLI, or built from MIABI_CLI_SRC; skipped when neither exists.
mkdir -p "$WORK/bin"
MIABI_CLI_SRC=${MIABI_CLI_SRC:-$HOME/Projects/oss/Miabi-Platform/cli}
if [ -n "${MIABI_CLI:-}" ]; then ln -s "$MIABI_CLI" "$WORK/bin/miabi"
elif [ -d "$MIABI_CLI_SRC" ]; then (cd "$MIABI_CLI_SRC" && GOWORK=off go build -o "$WORK/bin/miabi" .) || true
fi
(cd "$AGENT_DIR" && go build -o "$WORK/akili-agent" ./cmd/akili-agent)
"$WORK/fakemiabi" -addr "127.0.0.1:$MIABI_PORT" -key "$MKEY" -tags v1,v2 >"$WORK/miabi.log" 2>&1 &
MIABI_PID=$!
AKILI_ENV=development AKILI_PORT=$PORT AKILI_PUBLIC_URL=$BASE AKILI_ENV_FILE=/dev/null \
AKILI_DATABASE_URL="postgres://akili:akili@127.0.0.1:$PG_PORT/akili?sslmode=disable" \
AKILI_REDIS_ADDR="127.0.0.1:$REDIS_PORT" AKILI_ADMIN_EMAIL=admin@e2e.local AKILI_ADMIN_PASSWORD=$PASS \
PATH="$WORK/bin:$PATH" ANTHROPIC_API_KEY= "$WORK/akili" server >"$WORK/server.log" 2>&1 &
SERVER_PID=$!
wait_for "control plane" 60 curl -fsS "$BASE/healthz"
wait_for "fake miabi" 30 curl -fsS "$MIABI/_fake/state"
api POST /auth/login "{\"email\":\"admin@e2e.local\",\"password\":\"$PASS\"}" >/dev/null

step "Agent with the operator policy (Miabi tools) at L2"
POLICY=$(api GET /policies | json "[p['id'] for p in d['data'] if p['name']=='operator'][0]")
created=$(api POST /agents "{\"name\":\"ops-miabi\",\"policy_id\":\"$POLICY\",\"autonomy\":2}")
AGENT=$(echo "$created" | json "d['data']['agent']['id']")
TOKEN=$(echo "$created" | json "d['data']['join_token']")
mkdir -p "$WORK/st" "$WORK/wk"
AKILI_JOIN_TOKEN=$TOKEN AKILI_LOG_FORMAT=text "$WORK/akili-agent" enroll --url "$BASE" --state-dir "$WORK/st" --workdir "$WORK/wk" >>"$WORK/agent.log" 2>&1
AKILI_LOG_FORMAT=text "$WORK/akili-agent" run --state-dir "$WORK/st" >>"$WORK/agent.log" 2>&1 &
AGENT_PID=$!
online() { [ "$(api GET /agents/$AGENT | json "d['data']['status']")" = "online" ]; }
wait_for "agent online" 30 online

step "Miabi integration with an account-wide key: workspaces are discovered, none enabled yet"
INT=$(api POST /integrations "{\"name\":\"miabi\",\"kind\":\"miabi\",\"base_url\":\"$MIABI\",\"token\":\"$MKEY\",\"webhook_secret\":\"$HOOK_SECRET\"}" | json "d['data']['id']")
api GET /integrations | grep >/dev/null "$MKEY" && fail "the Miabi key was returned by the API"
api GET /integrations | json "[i['webhook_url'] for i in d['data'] if i['id']=='$INT'][0]" | grep >/dev/null "/webhooks/miabi/$INT" || fail "webhook URL"
R=$(code POST /projects "{\"name\":\"x\",\"integration_id\":\"$INT\",\"owner\":\"x\",\"repo\":\"y\"}"); [ "$R" -ge 400 ] || fail "a Miabi integration was accepted as a git forge ($R: $(cat "$WORK/code.out"))"
[ "$(api GET /integrations/$INT/miabi-workspaces | json "sorted((w['name'], w['accessible'], w['enabled']) for w in d['data'])")" = "[('prod', True, False), ('staging', True, False)]" ] || fail "workspaces: $(api GET /integrations/$INT/miabi-workspaces)"
ws_id() { api GET /integrations/$INT/miabi-workspaces | json "[w['id'] for w in d['data'] if w['name']=='$1'][0]"; }
STAGING=$(ws_id staging); PROD=$(ws_id prod)
for w in "$STAGING" "$PROD"; do api PUT "/integrations/$INT/miabi-workspaces/$w" '{"enabled":true}' >/dev/null; done
[ "$(api POST /integrations/$INT/test | json "d['data']['ok']")" = "True" ] || fail "integration test: $(api POST /integrations/$INT/test)"
api POST /integrations/$INT/test | json "d['data']['reply']" | grep >/dev/null "2 workspaces reachable, 2 enabled" || fail "test summary: $(api POST /integrations/$INT/test)"

BADCA="{\"name\":\"bad-ca\",\"kind\":\"miabi\",\"base_url\":\"$MIABI\",\"token\":\"$MKEY\",\"ca_cert\":\"not a certificate\"}"
[ "$(code POST /integrations "$BADCA")" = "400" ] || fail "an invalid CA certificate was accepted"
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 1 -subj "/CN=miabi-ca" -keyout "$WORK/mca.key" -out "$WORK/mca.pem" 2>/dev/null
CABODY=$(python3 -c 'import json,sys; print(json.dumps({"name":"miabi","kind":"miabi","base_url":sys.argv[1],"ca_cert":open(sys.argv[2]).read()}))' "$MIABI" "$WORK/mca.pem")
api PUT /integrations/$INT "$CABODY" >/dev/null
api GET /integrations | json "[i['ca_cert'] for i in d['data'] if i['id']=='$INT'][0]" | grep >/dev/null "BEGIN CERTIFICATE" || fail "CA certificate not stored"
[ "$(api POST /integrations/$INT/test | json "d['data']['ok']")" = "True" ] || fail "an extra CA broke the plain-HTTP connection"
api PUT /integrations/$INT '{"name":"miabi","kind":"miabi","base_url":"'"$MIABI"'","ca_cert":""}' >/dev/null
[ -z "$(api GET /integrations | json "[i['ca_cert'] for i in d['data'] if i['id']=='$INT'][0]")" ] || fail "CA certificate not removed"

step "Watches: every app in staging (scripted verification) + databases; prod/api for failures"
python3 - "$WORK/watch.json" "$INT" "$AGENT" <<'PY2'
import json, sys
app = {"workspace": "staging", "app": "api"}
plan = {"title": "Roll back api", "reason": "the new release is unhealthy; the rollback restores the previous release",
        "steps": [{"tool": "miabi_rollback", "input": app}],
        "verify": [{"tool": "miabi_status", "input": app, "expect": "health: healthy"}], "rollback": []}
json.dump({"integration_id": sys.argv[2], "workspace": "staging", "app": "*", "databases": True, "agent_id": sys.argv[3],
           "instructions": "tool: miabi_status " + json.dumps(app) + "\nchange: " + json.dumps(plan)}, open(sys.argv[1], "w"))
PY2
WATCH=$(api_file POST /miabi-watches "$WORK/watch.json")
[ "$(echo "$WATCH" | json "(d['data']['workspace'], d['data']['app'], d['data']['databases'])")" = "('staging', '*', True)" ] || fail "staging watch: $WATCH"
PWATCH=$(api POST /miabi-watches "{\"integration_id\":\"$INT\",\"workspace\":\"prod\",\"app\":\"api\",\"verify_deploys\":false,\"agent_id\":\"$AGENT\",\"instructions\":\"tool: miabi_status {\\\"workspace\\\":\\\"prod\\\",\\\"app\\\":\\\"api\\\"}\"}")
[ "$(echo "$PWATCH" | json "d['data']['app_id']")" = "11" ] || fail "prod watch did not resolve the app: $PWATCH"
BAD_WATCH="{\"integration_id\":\"$INT\",\"workspace\":\"prod\",\"app\":\"nope\"}"
[ "$(code POST /miabi-watches "$BAD_WATCH")" -ge 400 ] || fail "watch on an unknown app accepted: $(cat "$WORK/code.out")"
NO_WS="{\"integration_id\":\"$INT\",\"app\":\"api\"}"
[ "$(code POST /miabi-watches "$NO_WS")" -ge 400 ] || fail "a watch without a workspace was accepted with two enabled"
streaming() { [ "$(api GET /integrations/$INT/miabi-workspaces | json "sorted(w['name'] for w in d['data'] if w['streaming'])")" = "['prod', 'staging']" ]; }
wait_for "event streams" 40 streaming

step "A) staging deploys v3 (unhealthy); the event stream opens a verification task"
curl -sS -X POST "$MIABI/_fake/unhealthy" -d '{"tag":"v3","unhealthy":true}' >/dev/null
curl -sS -X POST -H "Authorization: Bearer $MKEY" -d '{"tag":"v3"}' "$MIABI/api/v1/workspaces/staging/apps/1/deploy" >/dev/null
verify_task() { api GET /tasks | json "[t['id'] for t in d['data'] if t['trigger']=='miabi' and t['title']=='Verify deploy of staging/api'][0]"; }
wait_for "verification task from the stream" 30 verify_task
T1=$(verify_task)
[ "$(active_tag)" = "v3" ] || fail "fake Miabi did not deploy v3"

step "The agent finds it unhealthy and proposes a rollback plan"
pending() { [ "$(api GET '/changes?status=pending' | json "len(d['data'])")" -ge 1 ]; }
wait_for "rollback plan" 60 pending
CHG=$(api GET '/changes?status=pending' | json "d['data'][0]['id']")
[ "$(active_tag)" = "v3" ] || fail "rolled back before approval"
SES=$(api GET /tasks/$T1 | json "d['data']['session_id']")
api GET "/sessions/$SES" | json "[e['payload'].get('output','') for e in d['data']['events'] if e['type']=='tool.result' and e['payload'].get('tool')=='miabi_status'][0]" | grep >/dev/null "health: unhealthy" || fail "status did not show the unhealthy release"
api POST "/approvals/$(api GET /changes/$CHG | json "d['data']['approval_id']")/approve" '{}' >/dev/null

step "Approved: rolled back to v2 and verified healthy (the rollback opens no new verification)"
done_change() { [ "$(api GET /changes/$CHG | json "d['data']['status']")" = "succeeded" ]; }
wait_for "rollback change" 60 done_change
[ "$(active_tag)" = "v2" ] || fail "active release after rollback is $(active_tag)"
[ "$(api GET '/audit?action=miabi.rollback' | json "d['data']['items'][0]['metadata']['workspace']")" = "staging" ] || fail "rollback not audited with its workspace"
task_done() { [ "$(api GET /tasks/$T1 | json "d['data']['status']")" = "succeeded" ]; }
wait_for "verification task" 60 task_done
sleep 3
[ "$(api GET /tasks | json "len([t for t in d['data'] if t['title']=='Verify deploy of staging/api'])")" = "1" ] || fail "the rollback triggered another verification"

step "Signed webhooks still work: prod deploy.failed → triage; duplicates and unwatched events skipped"
EV='{"event":"deploy.failed","workspace_id":8,"subject_type":"app","application_id":11,"application_name":"api","application_slug":"api","severity":"error","message":"deployment #3 failed","metadata":{"deployment_id":301},"timestamp":"2026-10-01T12:00:00Z"}'
[ "$(miabi_hook "$EV" wrong-secret)" = "401" ] || fail "bad signature accepted"
[ "$(miabi_hook "$EV")" = "201" ] || fail "triage task not created: $(cat "$WORK/hook.out")"
[ "$(miabi_hook "$EV")" = "200" ] || fail "duplicate event created a second task"
[ "$(miabi_hook '{"event":"deploy.failed","workspace_id":8,"application_id":12,"application_slug":"other"}')" = "200" ] && grep -q ignored "$WORK/hook.out" || fail "unwatched app not ignored"
[ "$(miabi_hook '{"event":"webhook.test"}')" = "200" ] || fail "test event"
[ "$(api GET /tasks | json "len([t for t in d['data'] if t['title']=='Miabi deploy.failed: prod/api'])")" = "1" ] || fail "prod triage task"

step "Database events: backup.failed in staging opens a triage task"
curl -sS -o /dev/null -X POST -d '{"workspace":"staging","type":"backup.failed","database":"pg-main","message":"backup to s3 failed"}' "$MIABI/_fake/event"
db_task() { api GET /tasks | json "[t['id'] for t in d['data'] if t['title']=='Miabi backup.failed: staging/pg-main'][0]"; }
wait_for "database triage task" 30 db_task

step "B) The agent deploys prod v4 with a change plan; the check fails and the runtime rolls back"
curl -sS -X POST "$MIABI/_fake/unhealthy" -d '{"tag":"v4","unhealthy":true}' >/dev/null
python3 - "$WORK/task.json" "$AGENT" <<'PY2'
import json, sys
app = {"workspace": "prod", "app": "api"}
plan = {"title": "Deploy api v4", "reason": "release v4",
        "steps": [{"tool": "miabi_deploy", "input": dict(app, tag="v4")}],
        "verify": [{"tool": "miabi_status", "input": app, "expect": "health: healthy"}],
        "rollback": [{"tool": "miabi_rollback", "input": app}]}
json.dump({"goal": "change: " + json.dumps(plan), "agent_id": sys.argv[2], "autonomy": 2}, open(sys.argv[1], "w"))
PY2
T2=$(api_file POST /tasks "$WORK/task.json" | json "d['data']['id']")
plan_pending() { api GET '/changes?status=pending' | json "[c['id'] for c in d['data'] if c['title']=='Deploy api v4'][0]"; }
wait_for "deploy plan" 60 plan_pending
CHG2=$(plan_pending)
api POST "/approvals/$(api GET /changes/$CHG2 | json "d['data']['approval_id']")/approve" '{}' >/dev/null
rolled() { [ "$(api GET /changes/$CHG2 | json "d['data']['status']")" = "rolled_back" ]; }
wait_for "automatic rollback" 90 rolled
[ "$(active_tag prod)" = "v2" ] || fail "after the failed deploy prod runs $(active_tag prod), want v2"
[ "$(active_tag staging)" = "v2" ] || fail "the prod deploy touched staging"

step "C) Workspace operations: overview, alerts, traffic, env, scale, maintenance, canary, stack, cron, pipeline, backups"
run_task() { # goal → final result text
  local id; id=$(api POST /tasks "$(python3 -c 'import json,sys; print(json.dumps({"goal":sys.argv[1],"agent_id":sys.argv[2],"autonomy":3}))' "$1" "$AGENT")" | json "d['data']['id']")
  for _ in $(seq 60); do
    st=$(api GET /tasks/$id | json "d['data']['status']")
    case "$st" in succeeded|failed|cancelled|timed_out) api GET /tasks/$id | json "d['data']['result'] + d['data']['error']"; return;; esac
    sleep 1
  done
  echo "timeout"
}
expect_task() { # goal substring
  local out; out=$(run_task "$1")
  echo "$out" | grep -F >/dev/null -- "$2" || fail "task <$1> did not report <$2>: $out"
}
fstate() { curl -sS "$MIABI/_fake/state" | json "$1"; }
api PATCH /agents/$AGENT '{"autonomy":3}' >/dev/null
# The canary promotion below is a real deploy: keep the staging watch from verifying it meanwhile.
WATCH_ID=$(echo "$WATCH" | json "d['data']['id']")
api PUT /miabi-watches/$WATCH_ID "{\"integration_id\":\"$INT\",\"workspace\":\"staging\",\"app\":\"*\",\"verify_deploys\":false,\"databases\":true}" >/dev/null
expect_task 'tool: miabi_overview {"workspace":"staging"}' "workspace staging: 1 apps"
expect_task 'tool: miabi_overview {"workspace":"staging"}' "plan Team"
expect_task 'tool: miabi_alerts {"workspace":"staging"}' "High memory on api"
expect_task 'tool: miabi_alert {"workspace":"staging","alert":900,"action":"ack"}' "is now acknowledged"
[ "$(fstate "d['data']['staging']['alerts'][0]['state']")" = "acknowledged" ] || fail "alert not acknowledged in Miabi"
expect_task 'tool: miabi_events {"workspace":"staging"}' "deploy.succeeded"
expect_task 'tool: miabi_traffic {"workspace":"staging","app":"api"}' "traffic: ok"
curl -sS -o /dev/null -X POST "$MIABI/_fake/unhealthy" -d '{"tag":"v2","unhealthy":true}'
expect_task 'tool: miabi_traffic {"workspace":"staging","app":"api"}' "traffic: degraded (error rate 35.00% above 2.00%)"
curl -sS -o /dev/null -X POST "$MIABI/_fake/unhealthy" -d '{"tag":"v2","unhealthy":false}'
expect_task 'tool: miabi_env {"workspace":"staging","app":"api"}' "DATABASE_PASSWORD=<secret>"
run_task 'tool: miabi_env {"workspace":"staging","app":"api"}' | grep >/dev/null "••••" && fail "a masked secret value leaked through"
expect_task 'tool: miabi_env_set {"workspace":"staging","app":"api","key":"LOG_LEVEL","value":"debug"}' "deploy the app for it to take effect"
expect_task 'tool: miabi_env_set {"workspace":"staging","app":"api","key":"DATABASE_PASSWORD","value":"x"}' "is a secret"
[ "$(fstate "[e['value'] for e in d['data']['staging/api']['env'] if e['key']=='DATABASE_PASSWORD'][0]")" = "••••••••" ] || fail "a secret was overwritten"
expect_task 'tool: miabi_scale {"workspace":"staging","app":"api","replicas":3}' "scaled api to 3 replicas"
[ "$(fstate "d['data']['staging/api']['replicas']")" = "3" ] || fail "replicas not set in Miabi"
expect_task 'tool: miabi_maintenance {"workspace":"staging","app":"api","enabled":true,"message":"back soon"}' "maintenance: on"
[ "$(fstate "d['data']['staging/api']['maintenance']")" = "True" ] || fail "maintenance not on in Miabi"
expect_task 'tool: miabi_maintenance {"workspace":"staging","app":"api","enabled":false}' "maintenance: off"
expect_task 'tool: miabi_canary {"workspace":"staging","app":"api","action":"promote"}' "no canary deployment in progress"
curl -sS -o /dev/null -X POST -d '{"workspace":"staging"}' "$MIABI/_fake/canary"
expect_task 'tool: miabi_canary {"workspace":"staging","app":"api","action":"promote"}' "canary promotion: deployment"
expect_task 'tool: miabi_stack_restart {"workspace":"staging","name":"web"}' "api: ok"
expect_task 'tool: miabi_cronjobs {"workspace":"staging"}' "nightly-report (app api)"
expect_task 'tool: miabi_cron_run {"workspace":"staging","name":"nightly-report"}' "succeeded"
expect_task 'tool: miabi_pipelines {"workspace":"staging"}' "build-api"
expect_task 'tool: miabi_pipeline_run {"workspace":"staging","name":"build-api"}' "run #1 succeeded"
expect_task 'tool: miabi_databases {"workspace":"staging"}' "pg-main: postgres 17, running, health healthy"
expect_task 'tool: miabi_db_backup {"workspace":"staging","database":"pg-main","comment":"before change"}' "backup 1 of pg-main/app: completed"
expect_task 'tool: miabi_db_backups {"workspace":"staging","database":"pg-main"}' "backup 1: completed"
[ "$(api GET '/audit?action=miabi.db_backup' | json "d['data']['total']")" = "1" ] || fail "backup not audited"

step "Restore is critical: refused by the operator template; with a critical-risk policy it waits for a human"
OUT=$(run_task 'tool: miabi_db_restore {"workspace":"staging","database":"pg-main","backup":1}')
# Above the template's risk cap, the tool is not even offered to the model.
echo "$OUT" | grep >/dev/null "miabi_db_restore tool, which is not available" || fail "restore under the operator template: $OUT"
FULL=$(api GET /policies | json "[p['id'] for p in d['data'] if p['name']=='full-no-approval'][0]")
api PATCH /agents/$AGENT "{\"policy_id\":\"$FULL\"}" >/dev/null
RT=$(api POST /tasks "{\"goal\":\"tool: miabi_db_restore {\\\"workspace\\\":\\\"staging\\\",\\\"database\\\":\\\"pg-main\\\",\\\"backup\\\":1}\",\"agent_id\":\"$AGENT\",\"autonomy\":3}" | json "d['data']['id']")
restore_pending() { api GET '/approvals?status=pending' | json "[a['id'] for a in d['data'] if a['tool']=='miabi_db_restore'][0]"; }
wait_for "restore approval" 30 restore_pending
[ "$(fstate "d['data']['staging']['restored']")" = "0" ] || fail "restored before approval"
api POST "/approvals/$(restore_pending)/approve" '{}' >/dev/null
restored() { [ "$(fstate "d['data']['staging']['restored']")" = "1" ]; }
wait_for "restore" 30 restored
api PATCH /agents/$AGENT "{\"policy_id\":\"$POLICY\",\"autonomy\":2}" >/dev/null

step "Deploy verified by traffic: v5 has a high error rate; the plan's miabi_traffic check fails and it rolls back"
curl -sS -o /dev/null -X POST "$MIABI/_fake/unhealthy" -d '{"tag":"v5","unhealthy":true}'
python3 - "$WORK/task5.json" "$AGENT" <<'PY2'
import json, sys
app = {"workspace": "staging", "app": "api"}
plan = {"title": "Deploy api v5", "reason": "release v5",
        "steps": [{"tool": "miabi_deploy", "input": dict(app, tag="v5")}],
        "verify": [{"tool": "miabi_traffic", "input": dict(app, range="15m", max_error_rate=0.02), "expect": "traffic: ok"}],
        "rollback": [{"tool": "miabi_rollback", "input": app}]}
json.dump({"goal": "change: " + json.dumps(plan), "agent_id": sys.argv[2], "autonomy": 2}, open(sys.argv[1], "w"))
PY2
api_file POST /tasks "$WORK/task5.json" >/dev/null
v5_pending() { api GET '/changes?status=pending' | json "[c['id'] for c in d['data'] if c['title']=='Deploy api v5'][0]"; }
wait_for "v5 plan" 60 v5_pending
CHG5=$(v5_pending)
api POST "/approvals/$(api GET /changes/$CHG5 | json "d['data']['approval_id']")/approve" '{}' >/dev/null
rolled5() { [ "$(api GET /changes/$CHG5 | json "d['data']['status']")" = "rolled_back" ]; }
wait_for "rollback on bad traffic" 90 rolled5
[ "$(active_tag staging)" != "v5" ] || fail "v5 still active after its traffic check failed"

if [ -x "$WORK/bin/miabi" ]; then
step "D) MCP gateway: the real \`miabi mcp\` as a tool server (key stays on the control plane)"
MV=$(api POST /mcp-servers "{\"name\":\"miabi\",\"integration_id\":\"$INT\"}")
MCPS=$(echo "$MV" | json "d['data']['server']['id']")
[ -z "$(echo "$MV" | json "d['data'].get('error') or ''")" ] || fail "mcp server: $MV"
[ "$(echo "$MV" | json "sorted(t['name'] for t in d['data']['tools'] if t['enabled'] and t['risk']=='low')[:3]")" = "['get_app', 'get_database', 'get_deployment']" ] || fail "read-only tools not enabled: $MV"
api GET /mcp-servers | grep >/dev/null "$MKEY" && fail "the Miabi key leaked through the MCP server API"
# Explicit-list templates do not offer MCP tools: an admin opts in per policy.
MP=$(api GET /policies | python3 -c '
import json,sys
doc=[p["document"] for p in json.load(sys.stdin)["data"] if p["name"]=="operator"][0]
doc["name"]="operator-mcp"; doc["tools"]["allow"].append("mcp__miabi__*")
print(json.dumps({"name":"operator-mcp","document":doc}))')
MPID=$(api POST /policies "$MP" | json "d['data']['id']")
api PATCH /agents/$AGENT "{\"policy_id\":\"$MPID\",\"autonomy\":2}" >/dev/null
expect_task 'tool: mcp__miabi__list_workspaces {}' '"name": "prod"'
expect_task 'tool: mcp__miabi__list_apps {"workspace":"staging"}' '"image": "example/api"'
[ "$(api GET '/audit?action=mcp.call' | json "d['data']['total']")" -ge 2 ] || fail "MCP calls not audited"
LA=$(api GET /mcp-servers/$MCPS/tools | json "[t['id'] for t in d['data'] if t['name']=='list_apps'][0]")
api PUT /mcp-servers/$MCPS/tools/$LA '{"enabled":false,"risk":"low"}' >/dev/null
expect_task 'tool: mcp__miabi__list_apps {"workspace":"staging"}' "mcp__miabi__list_apps tool, which is not available"
api PUT /mcp-servers/$MCPS "{\"name\":\"miabi\",\"integration_id\":\"$INT\",\"allow_write\":true}" >/dev/null
api POST /mcp-servers/$MCPS/sync >/dev/null
RA=$(api GET /mcp-servers/$MCPS/tools | json "[t['id'] for t in d['data'] if t['name']=='restart_app'][0]")
[ "$(api GET /mcp-servers/$MCPS/tools | json "[(t['enabled'], t['risk']) for t in d['data'] if t['name']=='restart_app'][0]")" = "(False, '')" ] || fail "a write tool was enabled automatically"
[ "$(code PUT /mcp-servers/$MCPS/tools/$RA '{"enabled":true,"risk":"low"}')" = "400" ] || fail "a write tool was made low risk"
api PUT /mcp-servers/$MCPS/tools/$RA '{"enabled":true,"risk":"high"}' >/dev/null
RS=$(api POST /tasks "{\"goal\":\"tool: mcp__miabi__restart_app {\\\"workspace\\\":\\\"staging\\\",\\\"app\\\":\\\"api\\\"}\",\"agent_id\":\"$AGENT\",\"autonomy\":2}" | json "d['data']['id']")
mcp_pending() { api GET '/approvals?status=pending' | json "[a['id'] for a in d['data'] if a['tool']=='mcp__miabi__restart_app'][0]"; }
wait_for "approval for the MCP write tool" 30 mcp_pending
BEFORE=$(fstate "d['data']['staging/api']['restarts']")
api POST "/approvals/$(mcp_pending)/approve" '{}' >/dev/null
mcp_done() { [ "$(api GET /tasks/$RS | json "d['data']['status']")" = "succeeded" ]; }
wait_for "MCP restart" 30 mcp_done
[ "$(fstate "d['data']['staging/api']['restarts']")" = "$((BEFORE + 1))" ] || fail "the approved MCP restart did not reach Miabi"
api PATCH /agents/$AGENT "{\"policy_id\":\"$POLICY\"}" >/dev/null
else
printf '\n\033[33mskipping the MCP section: no miabi CLI (set MIABI_CLI or MIABI_CLI_SRC)\033[0m\n'
fi

step "Guards: workspace/app policy rules, names only, disabled and unreachable workspaces"
run_task 'tool: miabi_status {"workspace":"8","app":"api"}' | grep >/dev/null 'no Miabi workspace named "8"' || fail "a workspace id was accepted instead of its name"
run_task 'tool: miabi_status {"workspace":"prod","app":"11"}' | grep >/dev/null 'no Miabi app named "11"' || fail "an app id was accepted instead of its name"
OPDOC=$(api GET /policies | python3 -c '
import json,sys
doc=[p["document"] for p in json.load(sys.stdin)["data"] if p["name"]=="operator"][0]
doc["name"]="staging-only"; doc["apps"]={"allow":["staging/*"],"deny":["prod/*"]}
print(json.dumps({"name":"staging-only","document":doc}))')
SP=$(api POST /policies "$OPDOC" | json "d['data']['id']")
api PATCH /agents/$AGENT "{\"policy_id\":\"$SP\"}" >/dev/null
[ "$(api GET /agents/$AGENT | json "d['data']['policy_id']")" = "$SP" ] || fail "agent policy not switched"
OUT=$(run_task 'tool: miabi_restart {"workspace":"prod","app":"api"}')
[ "$(curl -sS "$MIABI/_fake/state" | json "d['data']['prod/api']['restarts']")" = "0" ] || fail "prod was restarted under a policy that denies prod/*: $OUT"
echo "$OUT" | grep -i >/dev/null "not allowed\|denied" || fail "the refusal was not reported: $OUT"
run_task 'tool: miabi_status {"workspace":"staging","app":"api"}' | grep >/dev/null "health: healthy" || fail "staging status under the staging-only policy"
api PATCH /agents/$AGENT "{\"policy_id\":\"$POLICY\"}" >/dev/null
api PUT "/integrations/$INT/miabi-workspaces/$PROD" '{"enabled":false}' >/dev/null
run_task 'tool: miabi_status {"workspace":"prod","app":"api"}' | grep >/dev/null 'not enabled for Akili' || fail "a disabled workspace was usable"
curl -sS -o /dev/null -X POST -d '{"workspace_id":7}' "$MIABI/_fake/bind"
api POST /integrations/$INT/miabi-workspaces/sync >/dev/null
[ "$(api GET /integrations/$INT/miabi-workspaces | json "sorted((w['name'], w['accessible']) for w in d['data'])")" = "[('prod', False), ('staging', True)]" ] || fail "binding not reflected: $(api GET /integrations/$INT/miabi-workspaces)"
[ "$(code PUT "/integrations/$INT/miabi-workspaces/$PROD" '{"enabled":true}')" = "400" ] || fail "an unreachable workspace could be enabled"

step "Default integration: the first is the default; tool calls without a name (or with \"default\") use it"
[ "$(api GET /integrations | json "[i['default'] for i in d['data'] if i['id']=='$INT'][0]")" = "True" ] || fail "the first Miabi integration is not the default"
INT2=$(api POST /integrations "{\"name\":\"miabi-two\",\"kind\":\"miabi\",\"base_url\":\"$MIABI\",\"token\":\"$MKEY\"}" | json "d['data']['id']")
[ "$(api GET /integrations | json "sorted((i['name'], i['default']) for i in d['data'] if i['kind']=='miabi')")" = "[('miabi', True), ('miabi-two', False)]" ] || fail "a second integration took the default: $(api GET /integrations)"
run_task 'tool: miabi_workspaces {}' | grep >/dev/null staging || fail "no-name call did not use the default"
run_task 'tool: miabi_workspaces {"integration":"default"}' | grep >/dev/null staging || fail "\"default\" did not resolve to the default"
OUT=$(run_task 'tool: miabi_workspaces {"integration":"nope"}')
echo "$OUT" | grep >/dev/null 'available: miabi, miabi-two' || fail "unknown name did not list the integrations: $OUT"
[ "$(code POST "/integrations/$INT2/default" '')" = "200" ] || fail "set default: $(cat "$WORK/code.out")"
[ "$(api GET /integrations | json "sorted((i['name'], i['default']) for i in d['data'] if i['kind']=='miabi')")" = "[('miabi', False), ('miabi-two', True)]" ] || fail "default did not move: $(api GET /integrations)"
api POST "/integrations/$INT/default" >/dev/null
api DELETE "/integrations/$INT2" >/dev/null

step "Audit chain verifies"
[ "$(api GET /audit/verify | json "d['data']['valid']")" = "True" ] || fail "audit chain invalid"

printf '\n\033[32mE2E MIABI PASSED\033[0m\n'
