#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Jonas Kaninda
# SPDX-License-Identifier: AGPL-3.0-or-later
# Chaos test: two control-plane replicas behind a load balancer share Postgres and Redis. A task is
# running when the replica holding the agent's tunnel is killed (SIGKILL). The agent reconnects
# through the load balancer to the surviving replica, which resumes the same task session from its
# history; the task finishes on its first attempt.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# The agent is its own repository, checked out next to this one.
AGENT_DIR="${AGENT_DIR:-$ROOT/agent}"
WORK="$(mktemp -d)"
PG_PORT=${PG_PORT:-55532}
REDIS_PORT=${REDIS_PORT:-56479}
LB_PORT=${LB_PORT:-18700}
PORTS=(18701 18702)
BASE="http://127.0.0.1:$LB_PORT"
API="$BASE/api/v1"
JAR="$WORK/cookies"
PASS="e2e-password-123456"
PIDS=(); AGENT_PID=""; LB_PID=""

cleanup() {
  for p in "$AGENT_PID" "$LB_PID" "${PIDS[@]:-}"; do [ -n "$p" ] && kill "$p" 2>/dev/null || true; done
  docker rm -f akili-ha-pg akili-ha-redis >/dev/null 2>&1 || true
  if [ "${KEEP:-}" = "1" ]; then echo "logs kept in $WORK"; else rm -rf "$WORK"; fi
}
trap cleanup EXIT

step() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }
fail() {
  printf '\033[31mFAIL: %s\033[0m\n' "$*"
  for i in 0 1; do echo "--- replica $i"; grep -v "Incoming request" "$WORK/server$i.log" | tail -15 || true; done
  echo "--- agent"; tail -20 "$WORK/agent.log" || true
  KEEP=1; exit 1
}
json() { python3 -c "import json,sys; d=json.load(sys.stdin); print(eval(sys.argv[1], {'d': d}))" "$1"; }
api() {
  local m=$1 p=$2 b=${3:-}
  if [ -n "$b" ]; then curl -sS -b "$JAR" -c "$JAR" -X "$m" -H 'Content-Type: application/json' -d "$b" "$API$p"
  else curl -sS -b "$JAR" -c "$JAR" -X "$m" "$API$p"; fi
}
wait_for() {
  local desc=$1 t=$2; shift 2
  for _ in $(seq "$t"); do if "$@" >/dev/null 2>&1; then return 0; fi; sleep 1; done
  fail "timed out waiting for $desc"
}
start_replica() {
  AKILI_ENV=development AKILI_PORT=${PORTS[$1]} AKILI_PUBLIC_URL=$BASE AKILI_ENV_FILE=/dev/null \
  AKILI_DATABASE_URL="postgres://akili:akili@127.0.0.1:$PG_PORT/akili?sslmode=disable" \
  AKILI_REDIS_ADDR="127.0.0.1:$REDIS_PORT" AKILI_ADMIN_EMAIL=admin@e2e.local AKILI_ADMIN_PASSWORD=$PASS \
  AKILI_JWT_SECRET=ha-e2e-jwt-secret-0123456789abcdef AKILI_ENCRYPTION_KEY=ha-e2e-encryption-key-0123456789abcdef \
  ANTHROPIC_API_KEY= "$WORK/akili" server >>"$WORK/server$1.log" 2>&1 &
  PIDS[$1]=$!
}

step "Postgres, Redis, two replicas and a load balancer"
docker rm -f akili-ha-pg akili-ha-redis >/dev/null 2>&1 || true
docker run -d --name akili-ha-pg -e POSTGRES_USER=akili -e POSTGRES_PASSWORD=akili -e POSTGRES_DB=akili -p "$PG_PORT:5432" postgres:17-alpine >/dev/null
docker run -d --name akili-ha-redis -p "$REDIS_PORT:6379" redis:7-alpine >/dev/null
wait_for "postgres" 60 docker exec akili-ha-pg pg_isready -U akili
(cd "$ROOT/server" && go build -o "$WORK/akili" ./cmd/akili && go build -o "$WORK/devlb" ./cmd/devlb)
(cd "$AGENT_DIR" && go build -o "$WORK/akili-agent" ./cmd/akili-agent)
start_replica 0
wait_for "replica 0" 60 curl -fsS "http://127.0.0.1:${PORTS[0]}/healthz"
start_replica 1
wait_for "replica 1" 60 curl -fsS "http://127.0.0.1:${PORTS[1]}/healthz"
"$WORK/devlb" -addr "127.0.0.1:$LB_PORT" -backends "127.0.0.1:${PORTS[0]},127.0.0.1:${PORTS[1]}" >"$WORK/lb.log" 2>&1 &
LB_PID=$!
wait_for "load balancer" 10 curl -fsS "$BASE/healthz"
api POST /auth/login "{\"email\":\"admin@e2e.local\",\"password\":\"$PASS\"}" >/dev/null

step "An agent enrolled through the load balancer"
POLICY=$(api GET /policies | json "[p['id'] for p in d['data'] if p['name']=='full-no-approval'][0]")
created=$(api POST /agents "{\"name\":\"ha-agent\",\"policy_id\":\"$POLICY\",\"autonomy\":3}")
AGENT=$(echo "$created" | json "d['data']['agent']['id']")
TOKEN=$(echo "$created" | json "d['data']['join_token']")
mkdir -p "$WORK/st" "$WORK/wk"
AKILI_JOIN_TOKEN=$TOKEN AKILI_LOG_FORMAT=text "$WORK/akili-agent" enroll --url "$BASE" --state-dir "$WORK/st" --workdir "$WORK/wk" >>"$WORK/agent.log" 2>&1
AKILI_LOG_FORMAT=text "$WORK/akili-agent" start --state-dir "$WORK/st" >>"$WORK/agent.log" 2>&1 &
AGENT_PID=$!
online() { [ "$(api GET /agents/$AGENT | json "d['data']['status']")" = "online" ]; }
wait_for "agent online" 30 online
holder() { for i in 0 1; do grep -q "agent connected" "$WORK/server$i.log" && echo "$i" && return; done; }
H=$(holder)
[ -n "$H" ] || fail "no replica logged the agent connection"
S=$((1 - H))
echo "tunnel on replica $H; replica $S survives"

step "A two-step task starts; the tunnel's replica is killed mid-step"
T=$(api POST /tasks "{\"goal\":\"run: sleep 8 && echo first-step\\nwrite: second.txt :: resumed after failover\",\"agent_id\":\"$AGENT\",\"autonomy\":3}" | json "d['data']['id']")
running() { [ "$(api GET /tasks/$T | json "d['data']['status']")" = "running" ]; }
wait_for "task running" 30 running
sleep 2
kill -9 "${PIDS[$H]}"
echo "killed replica $H"

step "The agent fails over and the surviving replica resumes the same session"
resumed() { grep -q "resumed task session" "$WORK/server$S.log"; }
wait_for "resume on replica $S" 60 resumed
done_task() { [ "$(api GET /tasks/$T | json "d['data']['status']")" = "succeeded" ]; }
wait_for "task to finish" 90 done_task
[ "$(cat "$WORK/wk/second.txt" 2>/dev/null)" = "resumed after failover" ] || fail "the second step did not run after the failover"
[ "$(api GET /tasks/$T | json "d['data']['attempts']")" = "1" ] || fail "the task was restarted (attempts=$(api GET /tasks/$T | json "d['data']['attempts']")), not resumed"
[ "$(api GET "/audit?action=session.resume" | json "(d.get('pageable') or {}).get('total_elements', len(d['data']))")" -ge 1 ] || fail "resume not audited"
[ "$(api GET "/audit?action=task.requeue" | json "(d.get('pageable') or {}).get('total_elements', len(d['data']))")" = "0" ] || fail "the task was requeued"

step "The killed replica restarts and the fleet keeps working"
start_replica "$H"
wait_for "replica $H back" 60 curl -fsS "http://127.0.0.1:${PORTS[$H]}/healthz"
T2=$(api POST /tasks "{\"goal\":\"write: after.txt :: still working\",\"agent_id\":\"$AGENT\",\"autonomy\":3}" | json "d['data']['id']")
done2() { [ "$(api GET /tasks/$T2 | json "d['data']['status']")" = "succeeded" ]; }
wait_for "task after restart" 60 done2
[ "$(api GET /audit/verify | json "d['data']['valid']")" = "True" ] || fail "audit chain invalid"

printf '\n\033[32mE2E HA PASSED\033[0m\n'
