#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Jonas Kaninda
# SPDX-License-Identifier: AGPL-3.0-or-later
# Load test: real Postgres + Redis (Docker), one control plane in development mode (scripted LLM,
# no key needed) and AGENTS simulated agents from akili-loadtest, which then run TASKS tasks.
#
#   AGENTS=1000 TASKS=200 scripts/loadtest.sh
#
# Other knobs: RAMP (connects/s, 100), HOLD (steady window, 60s). The per-IP enroll/connect limits
# count only failures, so the whole fleet can come from 127.0.0.1 with the limits on, as a fleet
# behind one NAT would. SERVER_BIN uses a prebuilt server. KEEP=1 keeps logs. Each agent needs ~3 fds
# here and ~2 in the server.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# The agent is its own repository, checked out next to this one.
AGENT_DIR="${AGENT_DIR:-$ROOT/agent}"
WORK="$(mktemp -d)"
AGENTS=${AGENTS:-1000}
TASKS=${TASKS:-200}
RAMP=${RAMP:-100}
HOLD=${HOLD:-60s}
PG_PORT=${PG_PORT:-55442}
REDIS_PORT=${REDIS_PORT:-56389}
PORT=${PORT:-18180}
BASE="http://127.0.0.1:$PORT"
PASS="loadtest-password-123456"
SERVER_PID=""; SAMPLER_PID=""

cleanup() {
  [ -n "$SAMPLER_PID" ] && kill "$SAMPLER_PID" 2>/dev/null || true
  [ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null || true
  docker rm -f akili-lt-pg akili-lt-redis >/dev/null 2>&1 || true
  if [ "${KEEP:-}" = "1" ]; then echo "logs kept in $WORK"; else rm -rf "$WORK"; fi
}
trap cleanup EXIT

step() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }
fail() { printf '\033[31mFAIL: %s\033[0m\n' "$*"; echo "--- server log"; tail -40 "$WORK/server.log" || true; KEEP=1; exit 1; }
wait_for() { # description timeout command...
  local desc=$1 t=$2; shift 2
  for _ in $(seq "$t"); do if "$@" >/dev/null 2>&1; then return 0; fi; sleep 1; done
  fail "timed out waiting for $desc"
}

want_fds=$((AGENTS * 3 + 512))
if [ "$(ulimit -n)" != "unlimited" ] && [ "$(ulimit -n)" -lt "$want_fds" ]; then
  ulimit -n "$want_fds" 2>/dev/null || ulimit -n "$(ulimit -Hn)" 2>/dev/null || true
fi
if [ "$(ulimit -n)" != "unlimited" ] && [ "$(ulimit -n)" -lt "$want_fds" ]; then
  echo "warning: ulimit -n is $(ulimit -n); $AGENTS agents need about $want_fds" >&2
fi

step "Starting Postgres and Redis"
docker rm -f akili-lt-pg akili-lt-redis >/dev/null 2>&1 || true
docker run -d --name akili-lt-pg -e POSTGRES_USER=akili -e POSTGRES_PASSWORD=akili -e POSTGRES_DB=akili -p "$PG_PORT:5432" postgres:17-alpine >/dev/null
docker run -d --name akili-lt-redis -p "$REDIS_PORT:6379" redis:7-alpine >/dev/null
wait_for "postgres" 60 docker exec akili-lt-pg pg_isready -U akili

step "Building"
if [ -n "${SERVER_BIN:-}" ]; then cp "$SERVER_BIN" "$WORK/akili"; else (cd "$ROOT/server" && go build -o "$WORK/akili" ./cmd/akili); fi
(cd "$AGENT_DIR" && go build -o "$WORK/akili-loadtest" ./cmd/akili-loadtest)

step "Starting the control plane"
AKILI_ENV=development AKILI_PORT=$PORT AKILI_PUBLIC_URL=$BASE \
AKILI_DATABASE_URL="postgres://akili:akili@127.0.0.1:$PG_PORT/akili?sslmode=disable" \
AKILI_REDIS_ADDR="127.0.0.1:$REDIS_PORT" AKILI_ADMIN_EMAIL=admin@loadtest.local AKILI_ADMIN_PASSWORD=$PASS \
AKILI_LOG_LEVEL=${SERVER_LOG_LEVEL:-warn} ANTHROPIC_API_KEY= AKILI_ENV_FILE=/dev/null "$WORK/akili" server >>"$WORK/server.log" 2>&1 &
SERVER_PID=$!
wait_for "control plane" 60 curl -fsS "$BASE/healthz"
curl -fsS "$BASE/readyz" >/dev/null || fail "not ready"


step "Running $AGENTS agents, $TASKS tasks"
(while sleep 2; do docker exec akili-lt-redis redis-cli INFO clients 2>/dev/null | tr -d '\r' | awk -F: '/^connected_clients/{print $2}'; done >"$WORK/redis-clients.txt") &
SAMPLER_PID=$!
status=0
AKILI_URL=$BASE AKILI_ADMIN_EMAIL=admin@loadtest.local AKILI_ADMIN_PASSWORD=$PASS \
  "$WORK/akili-loadtest" -agents "$AGENTS" -tasks "$TASKS" -ramp "$RAMP" -hold "$HOLD" \
  -work-dir "$WORK/state" -server-pid "$SERVER_PID" -cleanup ${LOADTEST_FLAGS:-} || status=$?

kill "$SAMPLER_PID" 2>/dev/null || true; wait "$SAMPLER_PID" 2>/dev/null || true; SAMPLER_PID=""
step "Backing services"
echo "redis clients max: $(sort -n "$WORK/redis-clients.txt" | tail -1)"
echo "redis clients end: $(docker exec akili-lt-redis redis-cli INFO clients | tr -d '\r' | awk -F: '/^connected_clients/{print $2}')"
echo "redis commands:    $(docker exec akili-lt-redis redis-cli INFO stats | tr -d '\r' | awk -F: '/^total_commands_processed/{print $2}')"
echo "postgres sessions: $(docker exec akili-lt-pg psql -U akili -tAc 'select count(*) from pg_stat_activity where datname = current_database()')"
echo "audit rows:        $(docker exec akili-lt-pg psql -U akili -tAc 'select count(*) from audit_logs' 2>/dev/null || echo n/a)"
echo "server log:        $(grep -ci 'error' "$WORK/server.log" || true) lines mention error"
grep -i 'error' "$WORK/server.log" | sed -E 's/"(time|ts)":"[^"]*",?//' | sort | uniq -c | sort -rn | head -5 || true

[ "$status" = "0" ] || fail "akili-loadtest exited $status"
printf '\n\033[32mLOAD TEST DONE\033[0m\n'
