#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Jonas Kaninda
# SPDX-License-Identifier: AGPL-3.0-or-later
# End-to-end test: real Postgres + Redis (Docker), the control plane and one agent, driven through
# the public API with the scripted provider (no LLM key needed).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# The agent is its own repository, checked out next to this one.
AGENT_DIR="${AGENT_DIR:-$ROOT/agent}"
WORK="$(mktemp -d)"
PG_PORT=${PG_PORT:-55432}
REDIS_PORT=${REDIS_PORT:-56379}
PORT=${PORT:-18080}
POSTA_PORT=${POSTA_PORT:-18770}
POSTA="http://127.0.0.1:$POSTA_PORT"
POSTA_KEY="psk_e2e_test_key"
BASE="http://127.0.0.1:$PORT"
API="$BASE/api/v1"
JAR="$WORK/cookies"
PASS="e2e-password-123456"
SERVER_PID=""; AGENT_PID=""; SERVER2_PID=""; POSTA_PID=""

cleanup() {
  [ -n "$AGENT_PID" ] && kill "$AGENT_PID" 2>/dev/null || true
  [ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null || true
  [ -n "$SERVER2_PID" ] && kill "$SERVER2_PID" 2>/dev/null || true
  [ -n "$POSTA_PID" ] && kill "$POSTA_PID" 2>/dev/null || true
  docker rm -f akili-e2e-pg akili-e2e-redis >/dev/null 2>&1 || true
  if [ "${KEEP:-}" = "1" ]; then echo "logs kept in $WORK"; else rm -rf "$WORK"; fi
}
trap cleanup EXIT

step() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }
fail() { printf '\033[31mFAIL: %s\033[0m\n' "$*"; echo "--- server log"; tail -40 "$WORK/server.log" || true; echo "--- agent log"; tail -40 "$WORK/agent.log" || true; KEEP=1; exit 1; }
json() { python3 -c "import json,sys; d=json.load(sys.stdin); print(eval(sys.argv[1], {'d': d}))" "$1"; }
api() { # method path [body]
  local m=$1 p=$2 b=${3:-}
  if [ -n "$b" ]; then curl -sS -b "$JAR" -c "$JAR" -X "$m" -H 'Content-Type: application/json' -d "$b" "$API$p"
  else curl -sS -b "$JAR" -c "$JAR" -X "$m" "$API$p"; fi
}
wait_for() { # description timeout command...
  local desc=$1 t=$2; shift 2
  for _ in $(seq "$t"); do if "$@" >/dev/null 2>&1; then return 0; fi; sleep 1; done
  fail "timed out waiting for $desc"
}

step "Starting Postgres and Redis"
docker rm -f akili-e2e-pg akili-e2e-redis >/dev/null 2>&1 || true
docker run -d --name akili-e2e-pg -e POSTGRES_USER=akili -e POSTGRES_PASSWORD=akili -e POSTGRES_DB=akili -p "$PG_PORT:5432" postgres:17-alpine >/dev/null
docker run -d --name akili-e2e-redis -p "$REDIS_PORT:6379" redis:7-alpine >/dev/null
wait_for "postgres" 60 docker exec akili-e2e-pg pg_isready -U akili

step "Building (Enterprise build, verifying licenses with a throwaway keypair)"
(cd "$ROOT/server" && go build -o "$WORK/akili-license" ./cmd/akili-license)
# A throwaway signing keypair for this run only (the real one is made by the private akili-keygen).
cat >"$WORK/testkey.go" <<'GO'
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

func main() {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	fmt.Printf("public_key: %s\nprivate_key: %s\n", base64.StdEncoding.EncodeToString(pub), base64.StdEncoding.EncodeToString(priv))
}
GO
(cd "$WORK" && GOWORK=off go build -o testkey testkey.go)
KEYPAIR=$("$WORK/testkey")
LICENSE_PUB=$(echo "$KEYPAIR" | awk '/public_key/{print $2}')
export AKILI_LICENSE_SIGNING_KEY=$(echo "$KEYPAIR" | awk '/private_key/{print $2}')
(cd "$ROOT/server" && go build -tags enterprise -ldflags "-X github.com/goakili/akili/server/internal/enterprise.embeddedPublicKey=$LICENSE_PUB" -o "$WORK/akili" ./cmd/akili)
(cd "$AGENT_DIR" && go build -o "$WORK/akili-agent" ./cmd/akili-agent)
(cd "$ROOT/server" && go build -o "$WORK/fakeposta" ./cmd/fakeposta)
"$WORK/fakeposta" -addr "127.0.0.1:$POSTA_PORT" -key "$POSTA_KEY" >"$WORK/posta.log" 2>&1 &
POSTA_PID=$!

step "Starting the control plane"
start_server() {
  AKILI_ENV=development AKILI_PORT=$PORT AKILI_PUBLIC_URL=$BASE \
  AKILI_DATABASE_URL="postgres://akili:akili@127.0.0.1:$PG_PORT/akili?sslmode=disable" \
  AKILI_REDIS_ADDR="127.0.0.1:$REDIS_PORT" AKILI_ADMIN_EMAIL=admin@e2e.local AKILI_ADMIN_PASSWORD=$PASS \
  ANTHROPIC_API_KEY= AKILI_ENV_FILE=/dev/null "$WORK/akili" server >>"$WORK/server.log" 2>&1 &
  SERVER_PID=$!
}
start_server
wait_for "control plane" 60 curl -fsS "$BASE/healthz"
curl -fsS "$BASE/readyz" >/dev/null || fail "not ready"

step "Install script: the agent comes from the GitHub release, verified against checksums.txt"
SCRIPT=$(curl -fsS "$BASE/install-agent.sh")
echo "$SCRIPT" | grep >/dev/null 'https://github.com/goakili/akili/releases' || fail "install script does not download from GitHub releases"
echo "$SCRIPT" | grep >/dev/null 'AKILI_AGENT_VERSION:-latest}' || fail "a dev build should install the latest agent release"
echo "$SCRIPT" | grep >/dev/null 'checksums.txt' || fail "install script does not verify the checksum"
echo "$SCRIPT" | grep >/dev/null '__AKILI_AGENT_VERSION__' && fail "version placeholder left in the install script"

step "Logging in"
login=$(api POST /auth/login "{\"email\":\"admin@e2e.local\",\"password\":\"$PASS\"}")
[ "$(echo "$login" | json "d['success']")" = "True" ] || fail "login: $login"
[ "$(api GET /auth/me | json "d['data']['user']['role']")" = "owner" ] || fail "me"
bad=$(curl -s -o /dev/null -w '%{http_code}' -X POST -H 'Content-Type: application/json' -d '{"email":"admin@e2e.local","password":"wrong-password-xx"}' "$API/auth/login")
[ "$bad" = "401" ] || fail "wrong password returned $bad"
[ "$(curl -s -o /dev/null -w '%{http_code}' "$API/agents")" = "401" ] || fail "unauthenticated request was allowed"

step "Editor sign-in: the browser approves, the editor redeems the code with its PKCE verifier"
VERIFIER=$(head -c 32 /dev/urandom | base64 | tr '+/' '-_' | tr -d '=\n')
CHALLENGE=$(printf %s "$VERIFIER" | openssl dgst -sha256 -binary | base64 | tr '+/' '-_' | tr -d '=\n')
VSC_STATE=e2e-state-0123456789
authz() { api POST /auth/vscode/authorize "{\"challenge\":\"$CHALLENGE\",\"state\":\"$VSC_STATE\",\"client\":\"VS Code on e2e\",\"editor\":\"$1\",\"window\":\"${2:-}\"}"; }
authz "https" | grep >/dev/null '"success":false' || fail "a web scheme was accepted for an editor sign-in"
authz "vscode" "1&code=forged" | grep >/dev/null '"success":false' || fail "a malformed window id was accepted"
# VS Code's windowId sends the callback back to the window that asked.
REDIRECT=$(authz "vscode" "1" | json "d['data']['redirect']")
case "$REDIRECT" in "vscode://goakili.akili/callback?code=akc_"*"&state=$VSC_STATE&windowId=1") ;; *) fail "unexpected editor redirect: $REDIRECT" ;; esac
CODE=$(echo "$REDIRECT" | sed 's/.*code=\([^&]*\).*/\1/')
redeem() { curl -sS -o "$WORK/vsc.json" -w '%{http_code}' -X POST -H 'Content-Type: application/json' -d "{\"code\":\"$1\",\"verifier\":\"$2\"}" "$API/auth/vscode/token"; }
[ "$(redeem "$CODE" "$VERIFIER")" = "201" ] || fail "redeeming the editor code: $(cat "$WORK/vsc.json")"
VSC_KEY=$(json "d['data']['secret']" <"$WORK/vsc.json")
[ "$(json "d['data']['key']['name']" <"$WORK/vsc.json")" = "VS Code on e2e" ] || fail "editor key name: $(cat "$WORK/vsc.json")"
[ "$(curl -sS -H "Authorization: Bearer $VSC_KEY" "$API/auth/me" | json "d['data']['user']['email']")" = "admin@e2e.local" ] || fail "the editor key does not act as the user"
[ "$(redeem "$CODE" "$VERIFIER")" = "401" ] || fail "an editor code was redeemed twice"
CODE2=$(authz "vscode" | json "d['data']['redirect']" | sed 's/.*code=\([^&]*\).*/\1/')
[ "$(redeem "$CODE2" "$(head -c 32 /dev/urandom | base64 | tr '+/' '-_' | tr -d '=\n')")" = "401" ] || fail "an editor code was redeemed with the wrong verifier"
[ "$(redeem "$CODE2" "$VERIFIER")" = "401" ] || fail "an editor code survived a wrong verifier"
chained=$(curl -sS -o "$WORK/vsc-chain.json" -w '%{http_code}' -X POST -H "Authorization: Bearer $VSC_KEY" -H 'Content-Type: application/json' \
  -d "{\"challenge\":\"$CHALLENGE\",\"state\":\"$VSC_STATE\",\"client\":\"chained\",\"editor\":\"vscode\"}" "$API/auth/vscode/authorize")
[ "$chained" = "403" ] || fail "an API key approved an editor sign-in: $chained $(cat "$WORK/vsc-chain.json")"
api GET "/audit?action=api_key.create" | grep >/dev/null '"source":"vscode"' || fail "editor key creation not audited"

step "Enterprise license: Community until installed; forged and foreign licenses refused"
put_license() { curl -sS -b "$JAR" -o "$WORK/license.json" -w '%{http_code}' -X PUT -H 'Content-Type: application/json' -d "{\"token\":\"$1\"}" "$API/license"; }
[ "$(api GET /license | json "(d['data']['edition'], d['data']['state'], d['data']['licensable'], len(d['data']['features']))")" = "('community', 'none', True, 15)" ] || fail "unlicensed edition: $(api GET /license)"
[ "$(put_license 'akili-v1.bm90.YQ')" = "400" ] || fail "a malformed license was accepted"
INSTALL_ID=$(api GET /license | json "d['data']['install_id']")
case "$INSTALL_ID" in ins_*) ;; *) fail "no install id: $INSTALL_ID" ;; esac
OTHER_INSTALL=$("$WORK/akili-license" sign --customer "Other Co" --install-id ins_someoneelse --flags saml)
[ "$(put_license "$OTHER_INSTALL")" = "400" ] && grep >/dev/null "install ID ins_someoneelse" "$WORK/license.json" || fail "a license for another install was accepted: $(cat "$WORK/license.json")"
FOREIGN=$("$WORK/akili-license" sign --customer "Other Co" --url https://akili.other.example.org --flags saml)
[ "$(put_license "$FOREIGN")" = "400" ] || fail "a license for another deployment was accepted"
FORGED=$(AKILI_LICENSE_SIGNING_KEY=$("$WORK/testkey" | awk '/private_key/{print $2}') "$WORK/akili-license" sign --customer Forger --flags all)
[ "$(put_license "$FORGED")" = "400" ] || fail "a license signed with another key was accepted"
LICENSE=$("$WORK/akili-license" sign --customer "Acme Corp" --install-id "$INSTALL_ID" --url "$BASE" --flags saml,scim --agents 25 --days 30)
[ "$(put_license "$LICENSE")" = "200" ] || fail "license install: $(cat "$WORK/license.json")"
[ "$(json "(d['data']['edition'], d['data']['state'], d['data']['customer'], d['data']['limits']['agents'])" <"$WORK/license.json")" = "('enterprise', 'valid', 'Acme Corp', 25)" ] || fail "installed license: $(cat "$WORK/license.json")"
[ "$(json "sorted(f['name'] for f in d['data']['features'] if f['granted'])" <"$WORK/license.json")" = "['saml', 'scim']" ] || fail "granted features: $(cat "$WORK/license.json")"
api GET '/audit?action=license.install' | json "d['data']['items'][0]['metadata']['customer']" | grep >/dev/null "Acme Corp" || fail "license install not audited"

step "Email through Posta: integration, test email, settings"
mails() { curl -s "$POSTA/_fake/sent" | python3 -c 'import json,sys; s=[m for m in json.load(sys.stdin) if not m["dry_run"]]; print(sum(1 for m in s if sys.argv[1] in m["subject"]))' "$1"; }
mail_body() { curl -s "$POSTA/_fake/sent" | python3 -c 'import json,sys; s=[m for m in json.load(sys.stdin) if sys.argv[1] in m["subject"]]; print(s[-1]["text"]+s[-1]["html"] if s else "")' "$1"; }
has_mail() { [ "$(mails "$1")" -ge 1 ]; }
[ "$(api GET /auth/notifications | json "d['data']['email_available']")" = "False" ] || fail "email available before Posta is set up"
[ "$(curl -s -o /dev/null -w '%{http_code}' -b "$JAR" -X POST "$API/auth/notifications/test")" = "409" ] || fail "test email without Posta"
BAD=$(api POST /integrations "{\"name\":\"posta-bad\",\"kind\":\"posta\",\"base_url\":\"$POSTA\",\"token\":\"psk_wrong\",\"sender\":\"Akili <akili@e2e.local>\"}" | json "d['data']['id']")
api POST "/integrations/$BAD/test" | json "d['data']['error']" | grep >/dev/null "invalid API key" || fail "a wrong Posta key passed the test"
api DELETE "/integrations/$BAD" >/dev/null
R=$(curl -s -w '\n%{http_code}' -b "$JAR" -X POST -H 'Content-Type: application/json' -d "{\"name\":\"p\",\"kind\":\"posta\",\"base_url\":\"$POSTA\",\"token\":\"x\",\"sender\":\"Akili <a@b.c>\\nBcc: x@y.z\"}" "$API/integrations")
[ "$(echo "$R" | tail -1)" = "400" ] && echo "$R" | grep >/dev/null "sender must be one line" || fail "a multi-line sender was accepted: $R"
PI=$(api POST /integrations "{\"name\":\"posta\",\"kind\":\"posta\",\"base_url\":\"$POSTA\",\"token\":\"$POSTA_KEY\",\"sender\":\"Akili <akili@e2e.local>\"}" | json "d['data']['id']")
[ "$(api POST "/integrations/$PI/test" | json "d['data']['ok']")" = "True" ] || fail "posta integration test"
api GET /integrations | grep >/dev/null "$POSTA_KEY" && fail "the Posta key was returned by the API"
[ "$(api GET /auth/notifications | json "d['data']['email_available']")" = "True" ] || fail "email not available with Posta"
api POST /auth/notifications/test | json "d['data']['message']" | grep >/dev/null "admin@e2e.local" || fail "test email"
has_mail "Akili test email" || fail "test email not received by Posta"

step "Creating an agent bound to the developer policy (autonomy L2)"
POLICY=$(api GET /policies | json "[p['id'] for p in d['data'] if p['name']=='developer'][0]")
[ "$(api GET /policies | json "sorted(p['name'] for p in d['data'] if p['builtin'])")" = "['developer', 'full-no-approval', 'full-with-approval', 'operator', 'operator-safe', 'read-only']" ] || fail "built-in policies not seeded"
created=$(api POST /agents "{\"name\":\"e2e-agent\",\"labels\":[\"env=test\"],\"policy_id\":\"$POLICY\",\"autonomy\":2}")
AGENT=$(echo "$created" | json "d['data']['agent']['id']")
TOKEN=$(echo "$created" | json "d['data']['join_token']")
echo "$created" | json "d['data']['docker_command']" | grep >/dev/null " jkaninda/akili-agent:latest$" || fail "a dev build should run the latest agent image: $created"
[ -n "$TOKEN" ] || fail "no join token: $created"

step "Enrolling and starting the agent"
mkdir -p "$WORK/agent-state" "$WORK/agent-work"
AKILI_JOIN_TOKEN=$TOKEN AKILI_LOG_FORMAT=text "$WORK/akili-agent" enroll --url "$BASE" --state-dir "$WORK/agent-state" --workdir "$WORK/agent-work" >>"$WORK/agent.log" 2>&1 || fail "enroll"
if AKILI_JOIN_TOKEN=$TOKEN "$WORK/akili-agent" enroll --url "$BASE" --state-dir "$WORK/agent-state2" >>"$WORK/agent.log" 2>&1; then fail "join token was accepted twice"; fi
AKILI_LOG_FORMAT=text "$WORK/akili-agent" start --state-dir "$WORK/agent-state" >>"$WORK/agent.log" 2>&1 &
AGENT_PID=$!
agent_online() { [ "$(api GET /agents/$AGENT | json "d['data']['status']")" = "online" ]; }
wait_for "agent online" 30 agent_online

task_status() { api GET /tasks/$1 | json "d['data']['status']"; }
wait_task() { # id expected timeout
  for _ in $(seq "$3"); do s=$(task_status "$1"); [ "$s" = "$2" ] && return 0
    case "$s" in succeeded|failed|cancelled|timed_out) fail "task $1 ended $s (wanted $2): $(api GET /tasks/$1)";; esac; sleep 1; done
  fail "task $1 stuck in $(task_status "$1")"
}

step "Lost connection: the agent reconnects by itself after a control-plane restart (no re-enrollment)"
kill "$SERVER_PID"; wait "$SERVER_PID" 2>/dev/null || true
agent_offline_now() { ! curl -fsS "$BASE/healthz" >/dev/null 2>&1; }
wait_for "control plane down" 10 agent_offline_now
sleep 3
start_server
wait_for "control plane back" 60 curl -fsS "$BASE/healthz"
wait_for "agent back online without a new token" 45 agent_online
[ "$(api GET /license | json "(d['data']['state'], d['data']['customer'], d['data']['agents_in_use'], d['data']['install_id'])")" = "('valid', 'Acme Corp', 1, '$INSTALL_ID')" ] || fail "license or install id not kept across the restart: $(api GET /license)"
[ "$(curl -s -o /dev/null -w '%{http_code}' -b "$JAR" -X DELETE "$API/license")" = "200" ] || fail "license removal"
[ "$(api GET /license | json "d['data']['edition']")" = "community" ] || fail "still Enterprise after removal"
[ "$(grep -c 'msg=enrolled' "$WORK/agent.log")" = "1" ] || fail "agent enrolled more than once"

step "Subscribing to the live event stream (SSE)"
curl -sN -b "$JAR" "$API/events/stream" >"$WORK/events.sse" 2>/dev/null &
SSE_PID=$!

step "Task 1: a medium-risk write runs automatically at L2"
T1=$(api POST /tasks "{\"title\":\"write a note\",\"goal\":\"write: note.txt :: hello from akili\",\"selector\":[\"env=test\"],\"autonomy\":2}" | json "d['data']['id']")
wait_task "$T1" succeeded 30
[ "$(cat "$WORK/agent-work/note.txt")" = "hello from akili" ] || fail "note.txt not written"
wait_for "task email" 15 has_mail "Task succeeded: write a note"
mail_body "Task succeeded: write a note" | grep >/dev/null "$BASE/tasks/$T1" || fail "task email has no link to the task"

step "Task 2: a high-risk shell command waits for approval, then runs"
T2=$(api POST /tasks "{\"title\":\"approval e2e\",\"goal\":\"run: echo approved-e2e\",\"agent_id\":\"$AGENT\",\"autonomy\":2}" | json "d['data']['id']")
pending() { [ "$(api GET '/approvals?status=pending' | json "len(d['data'])")" -ge 1 ]; }
wait_for "approval request" 30 pending
APPROVAL=$(api GET '/approvals?status=pending' | json "d['data'][0]['id']")
[ "$(api GET '/approvals?status=pending' | json "d['data'][0]['tool']")" = "shell" ] || fail "approval is not for shell"
wait_for "approval email" 15 has_mail "Approval needed: shell on"
mail_body "Approval needed: shell on" | grep >/dev/null "approved-e2e" && fail "the approval email contains the tool arguments"
mail_body "Approval needed: shell on" | grep >/dev/null "$BASE/approvals" || fail "approval email has no link"
api POST "/approvals/$APPROVAL/approve" '{"note":"ok for e2e"}' >/dev/null
wait_task "$T2" succeeded 30
api GET "/tasks/$T2" | json "d['data']['result']" | grep >/dev/null "approved-e2e" || fail "shell output missing from the task result"
second=$(curl -s -o /dev/null -w '%{http_code}' -b "$JAR" -X POST -H 'Content-Type: application/json' -d '{}' "$API/approvals/$APPROVAL/deny")
[ "$second" = "409" ] || fail "an approval was decided twice ($second)"

step "Task 3: a denied approval is not executed (and, with task email off, mails nothing)"
[ "$(api PUT /auth/notifications '{"email_tasks":false}' | json "d['data']['email_tasks']")" = "False" ] || fail "turning task email off"
T3=$(api POST /tasks "{\"title\":\"denied e2e\",\"goal\":\"run: touch should-not-exist\",\"agent_id\":\"$AGENT\",\"autonomy\":2}" | json "d['data']['id']")
wait_for "approval request" 30 pending
APPROVAL=$(api GET '/approvals?status=pending' | json "d['data'][0]['id']")
api POST "/approvals/$APPROVAL/deny" '{"note":"not today"}' >/dev/null
wait_task "$T3" succeeded 30
[ ! -e "$WORK/agent-work/should-not-exist" ] || fail "a denied command ran"
sleep 2
[ "$(mails "denied e2e")" = "0" ] || fail "task email sent although turned off"
[ "$(mails "Approval needed")" = "1" ] || fail "a second approval email within a minute (rate limit)"
api GET "/audit?action=notify.email" | json "d['data']['items'][0]['metadata']['posta_id']" | grep >/dev/null "^00000000-" || fail "email not audited with the Posta id"

step "Chat: policy denies reading /etc/shadow"
SES=$(api POST /sessions "{\"agent_id\":\"$AGENT\",\"title\":\"e2e chat\"}" | json "d['data']['id']")
api POST "/sessions/$SES/messages" '{"text":"read: /etc/shadow"}' >/dev/null
denied() { api GET "/sessions/$SES" | json "[e['payload']['effect'] for e in d['data']['events'] if e['type']=='tool.request']" | grep >/dev/null deny; }
wait_for "denial event" 30 denied
replied() { [ "$(api GET "/sessions/$SES" | json "len([m for m in d['data']['messages'] if m['role']=='assistant'])")" -ge 2 ]; }
wait_for "assistant reply after denial" 30 replied

step "Chat: second message in the same session keeps history"
api POST "/sessions/$SES/messages" '{"text":"hello again"}' >/dev/null
echoed() { api GET "/sessions/$SES" | json "[b.get('text','') for m in d['data']['messages'] for b in m['content']]" | grep >/dev/null "Echo: hello again"; }
wait_for "echo reply" 30 echoed

step "Chat: images are stored out of the history and resolved for the model"
python3 -c 'import struct,zlib,sys
row=b"\x00\xff\x00\x00"; c=lambda t,d: struct.pack(">I",len(d))+t+d+struct.pack(">I",zlib.crc32(t+d))
sys.stdout.buffer.write(b"\x89PNG\r\n\x1a\n"+c(b"IHDR",struct.pack(">IIBBBBB",1,1,8,2,0,0,0))+c(b"IDAT",zlib.compress(row))+c(b"IEND",b""))' >"$WORK/pixel.png"
upload() { curl -sS -b "$JAR" -o "$WORK/upload.json" -w '%{http_code}' -X POST -H 'Content-Type: application/octet-stream' --data-binary "@$1" "$API/sessions/$2/attachments"; }
[ "$(upload "$WORK/pixel.png" "$SES")" = "201" ] || fail "image upload refused: $(cat "$WORK/upload.json")"
ATT=$(json "d['data']['id']" <"$WORK/upload.json")
[ "$(json "d['data']['media_type']" <"$WORK/upload.json")" = "image/png" ] || fail "media type not sniffed as image/png"
printf '<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>' >"$WORK/evil.svg"
[ "$(upload "$WORK/evil.svg" "$SES")" = "415" ] || fail "an SVG was accepted as an image"
head -c $((6 * 1024 * 1024)) /dev/zero >"$WORK/big.bin"
[ "$(upload "$WORK/big.bin" "$SES")" = "413" ] || fail "an image over 5 MB was accepted"
curl -sS -b "$JAR" -D "$WORK/att.headers" -o "$WORK/pixel.out" "$API/sessions/$SES/attachments/$ATT"
cmp -s "$WORK/pixel.png" "$WORK/pixel.out" || fail "the downloaded image differs from the upload"
grep -qi '^content-type: image/png' "$WORK/att.headers" || fail "attachment served without its image type"
OTHER=$(api POST /sessions "{\"agent_id\":\"$AGENT\",\"title\":\"e2e other\"}" | json "d['data']['id']")
code=$(curl -sS -b "$JAR" -o /dev/null -w '%{http_code}' -X POST -H 'Content-Type: application/json' -d "{\"text\":\"images?\",\"attachments\":[\"$ATT\"]}" "$API/sessions/$OTHER/messages")
[ "$code" = "400" ] || fail "another session's attachment was accepted (HTTP $code)"
code=$(curl -sS -b "$JAR" -o /dev/null -w '%{http_code}' "$API/sessions/$OTHER/attachments/$ATT")
[ "$code" = "404" ] || fail "an attachment was served through another session (HTTP $code)"
api POST "/sessions/$SES/messages" "{\"text\":\"images?\",\"attachments\":[\"$ATT\"]}" | grep >/dev/null '"success":true' || fail "message with an image refused"
PNG_BYTES=$(wc -c <"$WORK/pixel.png" | tr -d ' ')
seen_image() { api GET "/sessions/$SES" | json "[b.get('text','') for m in d['data']['messages'] for b in m['content']]" | grep >/dev/null "images: image/png $PNG_BYTES bytes"; }
wait_for "the model to receive the image" 30 seen_image
api GET "/sessions/$SES" | json "[b['source'] for m in d['data']['messages'] for b in m['content'] if b['type']=='image']" >"$WORK/stored.txt"
grep -q "$ATT" "$WORK/stored.txt" || fail "the image block was not stored in the history"
grep -q "'data'" "$WORK/stored.txt" && fail "image bytes were stored in the history"
api POST "/sessions/$SES/messages" "{\"attachments\":[\"$ATT\"]}" | grep >/dev/null '"success":true' || fail "an image-only message was refused"

step "Live events arrived over SSE"
kill "$SSE_PID" 2>/dev/null || true
for ev in ready task.updated approval.created tool.request message session.state; do
  grep -q "\"type\":\"$ev\"" "$WORK/events.sse" || fail "no $ev event on the org stream"
done
grep -q '"type":"assistant.delta"' "$WORK/events.sse" && fail "token deltas leaked into the org stream"
curl -sN -b "$JAR" "$API/events/stream?session=$SES" >"$WORK/session.sse" 2>/dev/null &
SSE_PID=$!
sleep 1
api POST "/sessions/$SES/messages" '{"text":"stream me please"}' >/dev/null
streamed() { grep -q '"type":"assistant.delta"' "$WORK/session.sse"; }
wait_for "streamed deltas on the focused org stream" 20 streamed
api POST "/sessions/$SES/messages" '{"text":"list: ."}' >/dev/null
thought() { grep -q '"kind":"thinking"' "$WORK/session.sse"; }
wait_for "reasoning deltas on the focused org stream" 20 thought
kill "$SSE_PID" 2>/dev/null || true

step "Multi-replica: a message sent through replica B reaches the agent tunnelled to replica A"
PORT2=$((PORT + 1))
AKILI_ENV=development AKILI_PORT=$PORT2 AKILI_PUBLIC_URL=$BASE \
AKILI_DATABASE_URL="postgres://akili:akili@127.0.0.1:$PG_PORT/akili?sslmode=disable" \
AKILI_REDIS_ADDR="127.0.0.1:$REDIS_PORT" AKILI_ENV_FILE=/dev/null "$WORK/akili" server >"$WORK/server2.log" 2>&1 &
SERVER2_PID=$!
wait_for "replica B" 60 curl -fsS "http://127.0.0.1:$PORT2/healthz"
curl -sS -b "$JAR" -X POST -H 'Content-Type: application/json' -d '{"text":"via replica b"}' "http://127.0.0.1:$PORT2/api/v1/sessions/$SES/messages" | grep >/dev/null '"success":true' || fail "replica B could not route the message"
via_b() { api GET "/sessions/$SES" | json "[b.get('text','') for m in d['data']['messages'] for b in m['content']]" | grep >/dev/null "Echo: via replica b"; }
wait_for "reply to the message sent via replica B" 30 via_b
kill "$SERVER2_PID" 2>/dev/null || true; SERVER2_PID=""

step "Usage and overview"
[ "$(api GET /overview | json "d['data']['tasks']['succeeded_24h']")" -ge 3 ] || fail "overview counts"
api GET /usage | json "d['data'][0]['calls']" >/dev/null || fail "usage"

step "Two-factor sign-in: TOTP enrolment, single-use codes, recovery codes, admin reset"
totp() { python3 -c "import hmac,hashlib,base64,struct,time,sys; k=base64.b32decode(sys.argv[1]+'='*(-len(sys.argv[1])%8)); c=int(time.time())//30+int(sys.argv[2]); h=hmac.new(k,struct.pack('>Q',c),hashlib.sha1).digest(); o=h[-1]&15; print('%06d'%((struct.unpack('>I',h[o:o+4])[0]&0x7fffffff)%1000000))" "$1" "${2:-0}"; }
MFA_PASS=mfa-e2e-password-123
MFA_USER=$(api POST /users "{\"email\":\"mfa@e2e.local\",\"role\":\"viewer\",\"password\":\"$MFA_PASS\"}" | json "d['data']['id']")
MJAR="$WORK/mfa.jar"
mapi() { JAR="$MJAR" api "$@"; }
mlogin() { mapi POST /auth/login "{\"email\":\"mfa@e2e.local\",\"password\":\"$MFA_PASS\"}"; }
mlogin | json "d['data']['mfa_required'] if 'mfa_required' in d['data'] else False" | grep >/dev/null False || fail "a user without 2FA got a challenge"
mapi POST /auth/2fa/setup '{"password":"wrong-password-xx"}' | grep >/dev/null '"success":false' || fail "2FA setup accepted a wrong password"
SETUP=$(mapi POST /auth/2fa/setup "{\"password\":\"$MFA_PASS\"}")
SECRET=$(echo "$SETUP" | json "d['data']['secret']")
echo "$SETUP" | json "d['data']['qr_code']" | grep >/dev/null '^data:image/png;base64,' || fail "no QR code in the setup"
mapi POST /auth/2fa/enable '{"code":"000000"}' | grep >/dev/null '"success":false' || fail "a wrong code enabled 2FA"
USED=$(totp "$SECRET")
RECOVERY=$(mapi POST /auth/2fa/enable "{\"code\":\"$USED\"}" | json "d['data']['recovery_codes'][0]")
[ -n "$RECOVERY" ] || fail "no recovery codes"
[ "$(mapi GET /auth/2fa | json "d['data']['recovery_codes_left']")" = "10" ] || fail "2FA status"
CH=$(mlogin)
[ "$(echo "$CH" | json "d['data']['mfa_required']")" = "True" ] && ! echo "$CH" | grep -q '"token"' || fail "the password alone started a session: $CH"
MTOK=$(echo "$CH" | json "d['data']['mfa_token']")
mapi POST /auth/login/2fa "{\"mfa_token\":\"$MTOK\",\"code\":\"$USED\"}" | grep >/dev/null '"success":false' || fail "a TOTP code was accepted twice"
mapi POST /auth/login/2fa "{\"mfa_token\":\"$MTOK\",\"code\":\"$(totp "$SECRET" 1)\"}" | json "d['data']['user']['email']" | grep >/dev/null mfa@e2e.local || fail "a fresh TOTP code was refused"
mapi POST /auth/login/2fa "{\"mfa_token\":\"$MTOK\",\"code\":\"$(totp "$SECRET" 1)\"}" | grep >/dev/null '"success":false' || fail "a challenge was used twice"
MTOK=$(mlogin | json "d['data']['mfa_token']")
mapi POST /auth/login/2fa "{\"mfa_token\":\"$MTOK\",\"code\":\"$RECOVERY\"}" | grep >/dev/null '"success":true' || fail "the recovery code was refused"
MTOK=$(mlogin | json "d['data']['mfa_token']")
mapi POST /auth/login/2fa "{\"mfa_token\":\"$MTOK\",\"code\":\"$RECOVERY\"}" | grep >/dev/null '"success":false' || fail "a recovery code was accepted twice"
[ "$(mapi GET /auth/2fa | json "d['data']['recovery_codes_left']")" = "9" ] || fail "the recovery code was not consumed"
api GET /users | json "[u['totp_enabled'] for u in d['data'] if u['email']=='mfa@e2e.local'][0]" | grep >/dev/null True || fail "users list does not show 2FA"
api DELETE "/users/$MFA_USER/2fa" | grep >/dev/null '"success":true' || fail "admin 2FA reset"
mlogin | grep >/dev/null '"mfa_required":true' && fail "2FA still required after the reset"
api GET '/audit?action=user.2fa_reset' | grep >/dev/null "$MFA_USER" || fail "2FA reset not audited"
api GET '/audit?action=auth.mfa_failed' | grep >/dev/null "$MFA_USER" || fail "failed codes not audited"

step "Audit chain verifies"
v=$(api GET /audit/verify)
[ "$(echo "$v" | json "d['data']['valid']")" = "True" ] || fail "audit chain invalid: $v"
[ "$(api GET '/audit?action=tool.request' | json "d['data']['total']")" -ge 4 ] || fail "tool requests not audited"

step "Revocation cuts the tunnel and blocks reconnects"
api POST "/agents/$AGENT/revoke" >/dev/null
agent_offline() { [ "$(api GET /agents/$AGENT | json "d['data']['status']")" = "revoked" ]; }
wait_for "agent revoked" 15 agent_offline
sleep 3
grep -q "connection lost" "$WORK/agent.log" || fail "agent did not lose its tunnel"
reconnect_refused() { grep -q "refused this agent's identity" "$WORK/agent.log" && grep -q "agent connection refused" "$WORK/server.log"; }
wait_for "reconnect refused" 40 reconnect_refused
api GET '/audit?action=agent.connect_refused' | json "d['data']['items'][0]['metadata']['reason']" | grep >/dev/null "revoked" || fail "refused reconnect not audited with its reason"

step "API key with read scope cannot write"
KEY=$(api POST /api-keys '{"name":"ro","scopes":["read"]}' | json "d['data']['secret']")
[ "$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $KEY" "$API/agents")" = "200" ] || fail "read key cannot read"
[ "$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $KEY" -H 'Content-Type: application/json' -X POST -d '{"goal":"x"}' "$API/tasks")" = "403" ] || fail "read key could write"

printf '\n\033[32mE2E PASSED\033[0m\n'
