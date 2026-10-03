#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Jonas Kaninda
# SPDX-License-Identifier: AGPL-3.0-or-later
# Hardening end-to-end test:
#  - TLS with agent mTLS required: an agent without a client certificate is refused; with one it enrolls.
#  - SSO against a fake OIDC provider: sign-in, group → role mapping, unverified email and forged
#    state refused, password sign-in limited to the owner.
#  - SIEM: the audit trail reaches a JSON-lines file and a signed webhook, hash chain intact.
#  - KMS: secrets move from the local key to a real Vault Transit key; after rotation the control
#    plane runs with Vault alone (no AKILI_ENCRYPTION_KEY) and still decrypts every secret.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# The agent is its own repository, checked out next to this one.
AGENT_DIR="${AGENT_DIR:-$ROOT/agent}"
WORK="$(mktemp -d)"
PG_PORT=${PG_PORT:-55542}
REDIS_PORT=${REDIS_PORT:-56489}
VAULT_PORT=${VAULT_PORT:-18820}
PORT=${PORT:-18710}
OIDC_PORT=${OIDC_PORT:-18711}
MIABI_PORT=${MIABI_PORT:-18712}
HOOK_PORT=${HOOK_PORT:-18713}
BASE="https://127.0.0.1:$PORT"
API="$BASE/api/v1"
JAR="$WORK/cookies"
PASS="e2e-password-123456"
ENC_KEY="hardening-e2e-encryption-key-0123456789"
MKEY="mb_hardening_0123456789"
SERVER_PID=""; AGENT_PID=""; OIDC_PID=""; MIABI_PID=""; HOOK_PID=""

cleanup() {
  for p in "$AGENT_PID" "$SERVER_PID" "$OIDC_PID" "$MIABI_PID" "$HOOK_PID"; do [ -n "$p" ] && kill "$p" 2>/dev/null || true; done
  docker rm -f akili-h-pg akili-h-redis akili-h-vault >/dev/null 2>&1 || true
  if [ "${KEEP:-}" = "1" ]; then echo "logs kept in $WORK"; else rm -rf "$WORK"; fi
}
trap cleanup EXIT

step() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }
fail() { printf '\033[31mFAIL: %s\033[0m\n' "$*"; echo "--- server log"; grep -v "Incoming request" "$WORK/server.log" | tail -25 || true; echo "--- agent log"; tail -15 "$WORK/agent.log" 2>/dev/null || true; KEEP=1; exit 1; }
json() { python3 -c "import json,sys; d=json.load(sys.stdin); print(eval(sys.argv[1], {'d': d}))" "$1"; }
CURL=(curl -sS --cacert "$WORK/ca.pem")
api() {
  local m=$1 p=$2 b=${3:-}
  if [ -n "$b" ]; then "${CURL[@]}" -b "$JAR" -c "$JAR" -X "$m" -H 'Content-Type: application/json' -d "$b" "$API$p"
  else "${CURL[@]}" -b "$JAR" -c "$JAR" -X "$m" "$API$p"; fi
}
wait_for() {
  local desc=$1 t=$2; shift 2
  for _ in $(seq "$t"); do if "$@" >/dev/null 2>&1; then return 0; fi; sleep 1; done
  fail "timed out waiting for $desc"
}
start_server() { # extra env as arguments
  env AKILI_ENV=development AKILI_PORT=$PORT AKILI_PUBLIC_URL=$BASE AKILI_ENV_FILE=/dev/null \
    AKILI_DATABASE_URL="postgres://akili:akili@127.0.0.1:$PG_PORT/akili?sslmode=disable" \
    AKILI_REDIS_ADDR="127.0.0.1:$REDIS_PORT" AKILI_ADMIN_EMAIL=admin@e2e.local AKILI_ADMIN_PASSWORD=$PASS \
    AKILI_JWT_SECRET=hardening-e2e-jwt-secret-0123456789abc \
    AKILI_TLS_CERT_FILE="$WORK/server.pem" AKILI_TLS_KEY_FILE="$WORK/server.key" \
    AKILI_AGENT_CLIENT_CA_FILE="$WORK/ca.pem" AKILI_AGENT_MTLS=required \
    AKILI_OIDC_ISSUER="http://127.0.0.1:$OIDC_PORT" AKILI_OIDC_CLIENT_ID=akili AKILI_OIDC_CLIENT_SECRET=akili-sso-secret \
    AKILI_OIDC_NAME="Example SSO" AKILI_OIDC_ALLOWED_DOMAINS=example.com AKILI_OIDC_ROLE_MAP="akili-admins=admin,akili-ops=operator" \
    AKILI_OIDC_DISABLE_PASSWORD=true \
    AKILI_SIEM_FILE="$WORK/siem.jsonl" AKILI_SIEM_WEBHOOK_URL="http://127.0.0.1:$HOOK_PORT/ingest" AKILI_SIEM_WEBHOOK_SECRET=siem-secret \
    ANTHROPIC_API_KEY= "$@" "$WORK/akili" server >>"$WORK/server.log" 2>&1 &
  SERVER_PID=$!
  wait_for "control plane" 60 "${CURL[@]}" -fsS "$BASE/healthz"
}
stop_server() { kill "$SERVER_PID"; wait "$SERVER_PID" 2>/dev/null || true; SERVER_PID=""; }
keys() { env AKILI_ENV=development AKILI_ENV_FILE=/dev/null AKILI_DATABASE_URL="postgres://akili:akili@127.0.0.1:$PG_PORT/akili?sslmode=disable" "$@"; }

step "Certificates: a CA, a server certificate and an agent client certificate"
(
  cd "$WORK"
  openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 1 -subj "/CN=akili-e2e-ca" -keyout ca.key -out ca.pem 2>/dev/null
  openssl req -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -subj "/CN=127.0.0.1" -keyout server.key -out server.csr 2>/dev/null
  printf "subjectAltName=IP:127.0.0.1\nextendedKeyUsage=serverAuth\n" >server.ext
  openssl x509 -req -in server.csr -CA ca.pem -CAkey ca.key -CAcreateserial -days 1 -extfile server.ext -out server.pem 2>/dev/null
  openssl req -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -subj "/CN=akili-agent" -keyout client.key -out client.csr 2>/dev/null
  printf "extendedKeyUsage=clientAuth\n" >client.ext
  openssl x509 -req -in client.csr -CA ca.pem -CAkey ca.key -CAcreateserial -days 1 -extfile client.ext -out client.pem 2>/dev/null
)

step "Postgres, Redis, Vault (Transit), a fake OIDC provider, a fake Miabi and a SIEM receiver"
docker rm -f akili-h-pg akili-h-redis akili-h-vault >/dev/null 2>&1 || true
docker run -d --name akili-h-pg -e POSTGRES_USER=akili -e POSTGRES_PASSWORD=akili -e POSTGRES_DB=akili -p "$PG_PORT:5432" postgres:17-alpine >/dev/null
docker run -d --name akili-h-redis -p "$REDIS_PORT:6379" redis:7-alpine >/dev/null
docker run -d --name akili-h-vault --cap-add=IPC_LOCK -e VAULT_DEV_ROOT_TOKEN_ID=root -p "$VAULT_PORT:8200" hashicorp/vault >/dev/null
wait_for "postgres" 60 docker exec akili-h-pg pg_isready -U akili
wait_for "vault" 60 docker exec -e VAULT_ADDR=http://127.0.0.1:8200 akili-h-vault vault status
docker exec -e VAULT_ADDR=http://127.0.0.1:8200 -e VAULT_TOKEN=root akili-h-vault sh -c 'vault secrets enable transit && vault write -f transit/keys/akili' >/dev/null
(cd "$ROOT/server" && go build -o "$WORK/akili" ./cmd/akili && go build -o "$WORK/fakeoidc" ./cmd/fakeoidc && go build -o "$WORK/fakemiabi" ./cmd/fakemiabi)
(cd "$AGENT_DIR" && go build -o "$WORK/akili-agent" ./cmd/akili-agent)
"$WORK/fakeoidc" -addr "127.0.0.1:$OIDC_PORT" -issuer "http://127.0.0.1:$OIDC_PORT" >"$WORK/oidc.log" 2>&1 &
OIDC_PID=$!
"$WORK/fakemiabi" -addr "127.0.0.1:$MIABI_PORT" -key "$MKEY" >"$WORK/miabi.log" 2>&1 &
MIABI_PID=$!
python3 - "$HOOK_PORT" "$WORK/hook.jsonl" >"$WORK/hook.log" 2>&1 <<'PY' &
import hashlib, hmac, json, sys
from http.server import BaseHTTPRequestHandler, HTTPServer
port, out = int(sys.argv[1]), sys.argv[2]
class H(BaseHTTPRequestHandler):
    def do_POST(self):
        body = self.rfile.read(int(self.headers["Content-Length"]))
        want = "sha256=" + hmac.new(b"siem-secret", body, hashlib.sha256).hexdigest()
        if self.headers.get("X-Akili-Signature") != want:
            self.send_response(401); self.end_headers(); return
        with open(out, "a") as f:
            for e in json.loads(body)["events"]:
                f.write(json.dumps(e) + "\n")
        self.send_response(204); self.end_headers()
    def log_message(self, *a): pass
HTTPServer(("127.0.0.1", port), H).serve_forever()
PY
HOOK_PID=$!
wait_for "fake oidc" 20 curl -fsS "http://127.0.0.1:$OIDC_PORT/.well-known/openid-configuration"

step "Control plane over TLS: local KMS, SSO, SIEM sinks, agent mTLS required"
start_server env AKILI_ENCRYPTION_KEY=$ENC_KEY
[ "$(api GET /auth/providers | json "d['data']['sso_name']")" = "Example SSO" ] || fail "SSO not advertised"
api POST /auth/login "{\"email\":\"admin@e2e.local\",\"password\":\"$PASS\"}" | grep >/dev/null '"success":true' || fail "owner password login (break-glass) refused"

step "Agent mTLS: refused without a client certificate, enrolled with one"
POLICY=$(api GET /policies | json "[p['id'] for p in d['data'] if p['name']=='full-no-approval'][0]")
created=$(api POST /agents "{\"name\":\"mtls-agent\",\"policy_id\":\"$POLICY\",\"autonomy\":3}")
AGENT=$(echo "$created" | json "d['data']['agent']['id']")
TOKEN=$(echo "$created" | json "d['data']['join_token']")
mkdir -p "$WORK/st" "$WORK/wk"
if AKILI_JOIN_TOKEN=$TOKEN "$WORK/akili-agent" enroll --url "$BASE" --state-dir "$WORK/st" --workdir "$WORK/wk" >"$WORK/noca.log" 2>&1; then
  fail "an agent that does not trust the control plane's CA enrolled"
fi
grep -q "not signed by a trusted CA: set AKILI_CA_CERT" "$WORK/noca.log" || fail "no CA hint: $(cat "$WORK/noca.log")"
if AKILI_JOIN_TOKEN=$TOKEN "$WORK/akili-agent" enroll --url "$BASE" --ca-cert "$WORK/ca.pem" --state-dir "$WORK/st" --workdir "$WORK/wk" >>"$WORK/agent.log" 2>&1; then
  fail "an agent without a client certificate enrolled"
fi
grep -q "requires an agent client certificate" "$WORK/agent.log" || fail "the refusal did not explain the missing certificate"
[ "$(api GET "/audit?action=agent.enroll" | json "d['data']['total']")" = "0" ] || fail "the refused enrollment consumed the token"
export AKILI_CLIENT_CERT_FILE="$WORK/client.pem" AKILI_CLIENT_KEY_FILE="$WORK/client.key"
# The CA as base64 PEM (as a single-line form field would pass it); run then uses the saved copy.
AKILI_CA_CERT_PEM="$(base64 <"$WORK/ca.pem" | tr -d '\n')" AKILI_JOIN_TOKEN=$TOKEN "$WORK/akili-agent" enroll --url "$BASE" --state-dir "$WORK/st" --workdir "$WORK/wk" >>"$WORK/agent.log" 2>&1 || fail "enrollment with a client certificate failed"
cmp -s "$WORK/ca.pem" "$WORK/st/ca.pem" || fail "the CA was not saved in the state directory"
AKILI_LOG_FORMAT=text "$WORK/akili-agent" start --state-dir "$WORK/st" >>"$WORK/agent.log" 2>&1 &
AGENT_PID=$!
unset AKILI_CLIENT_CERT_FILE AKILI_CLIENT_KEY_FILE
online() { [ "$(api GET /agents/$AGENT | json "d['data']['status']")" = "online" ]; }
wait_for "agent online over mTLS" 30 online
T=$(api POST /tasks "{\"goal\":\"write: mtls.txt :: over mtls\",\"agent_id\":\"$AGENT\",\"autonomy\":3}" | json "d['data']['id']")
task_ok() { [ "$(api GET /tasks/$T | json "d['data']['status']")" = "succeeded" ]; }
wait_for "task over mTLS" 30 task_ok

step "SSO: sign-in, role from groups, refusals"
sso() { # jar → final URL after following the redirects
  "${CURL[@]}" -o /dev/null -w '%{url_effective}' -L -b "$1" -c "$1" "$API/auth/oidc/login"
}
setuser() { curl -sS -o /dev/null -X POST -d "$1" "http://127.0.0.1:$OIDC_PORT/_fake/user"; }
setuser '{"sub":"alice","email":"alice@example.com","email_verified":true,"name":"Alice","groups":["akili-ops"]}'
final=$(sso "$WORK/alice")
[[ "$final" == "$BASE/" ]] || fail "SSO did not land on the UI: $final"
me=$("${CURL[@]}" -b "$WORK/alice" "$API/auth/me")
[ "$(echo "$me" | json "d['data']['user']['email']")" = "alice@example.com" ] || fail "SSO session: $me"
[ "$(echo "$me" | json "d['data']['user']['role']")" = "operator" ] || fail "group akili-ops did not map to operator"
setuser '{"sub":"alice","email":"alice@example.com","email_verified":true,"name":"Alice","groups":["akili-admins"]}'
sso "$WORK/alice2" >/dev/null
[ "$("${CURL[@]}" -b "$WORK/alice2" "$API/auth/me" | json "d['data']['user']['role']")" = "admin" ] || fail "role not re-synced from groups"
setuser '{"sub":"mallory","email":"mallory@example.com","email_verified":false,"name":"M"}'
[[ "$(sso "$WORK/mallory")" == *"sso_error="*"not+verified"* ]] || fail "an unverified email signed in"
setuser '{"sub":"eve","email":"eve@evil.test","email_verified":true,"name":"Eve"}'
[[ "$(sso "$WORK/eve")" == *"sso_error="*"allowed+domain"* ]] || fail "a foreign domain signed in"
setuser '{"sub":"alice-2","email":"alice@example.com","email_verified":true,"name":"Imposter"}'
[[ "$(sso "$WORK/imp")" == *"sso_error="*"another+identity"* ]] || fail "a second IdP subject took over alice"
forged=$("${CURL[@]}" -o /dev/null -w '%{redirect_url}' "$API/auth/oidc/callback?state=forged&code=x")
[[ "$forged" == *"sso_error="* ]] || fail "a forged callback was not refused: $forged"
api POST /users '{"email":"bob@example.com","name":"Bob","role":"admin","password":"bob-password-123456"}' >/dev/null
[ "$("${CURL[@]}" -o /dev/null -w '%{http_code}' -X POST -H 'Content-Type: application/json' -d '{"email":"bob@example.com","password":"bob-password-123456"}' "$API/auth/login")" = "401" ] || fail "a non-owner used a password while SSO is mandatory"
[ "$(api GET "/audit?action=auth.sso_login" | json "d['data']['total']")" -ge 2 ] || fail "SSO logins not audited"
[ "$(api GET "/audit?action=auth.sso_denied" | json "d['data']['total']")" -ge 4 ] || fail "SSO refusals not audited"

step "SIEM: the audit trail reaches the file and the signed webhook"
TOTAL=$(api GET /audit/verify | json "d['data']['checked']")
file_caught_up() { [ "$(wc -l <"$WORK/siem.jsonl")" -ge "$TOTAL" ]; }
hook_caught_up() { [ -f "$WORK/hook.jsonl" ] && [ "$(wc -l <"$WORK/hook.jsonl")" -ge "$TOTAL" ]; }
wait_for "file sink" 30 file_caught_up
wait_for "webhook sink" 30 hook_caught_up
python3 - "$WORK/siem.jsonl" "$WORK/hook.jsonl" <<'PY' || fail "SIEM streams are incomplete or out of order"
import json, sys
for path in sys.argv[1:]:
    rows = [json.loads(l) for l in open(path)]
    ids = [r["id"] for r in rows]
    assert ids == sorted(set(ids)) and ids[0] == 1, f"{path}: ids not contiguous from 1"
    assert ids == list(range(1, len(ids) + 1)), f"{path}: gap in ids"
    for a, b in zip(rows, rows[1:]):
        assert b["prev_hash"] == a["hash"], f"{path}: chain broken at {b['id']}"
    assert all(r["source"] == "akili" for r in rows)
print("ok")
PY
[ "$(api GET /security | json "len(d['data']['siem'])")" = "2" ] || fail "sink status missing"
[ "$(api GET /security | json "max(s['lag'] for s in d['data']['siem'])")" -le 2 ] || fail "sinks lag behind"

step "KMS: store a secret, move data keys to Vault Transit, rotate, then run on Vault alone"
INT=$(api POST /integrations "{\"name\":\"miabi\",\"kind\":\"miabi\",\"base_url\":\"http://127.0.0.1:$MIABI_PORT\",\"workspace\":\"7\",\"token\":\"$MKEY\"}" | json "d['data']['id']")
[ "$(api POST /integrations/$INT/test | json "d['data']['ok']")" = "True" ] || fail "integration test (local KMS)"
[ "$(api GET /security | json "d['data']['kms']")" = "local" ] || fail "kms status"
stop_server
VAULT_ENV=(AKILI_KMS=vault-transit AKILI_VAULT_ADDR="http://127.0.0.1:$VAULT_PORT" AKILI_VAULT_TOKEN=root AKILI_VAULT_TRANSIT_KEY=akili)
keys "${VAULT_ENV[@]}" AKILI_ENCRYPTION_KEY=$ENC_KEY "$WORK/akili" keys rewrap | tee "$WORK/rewrap.out" | grep >/dev/null "rewrapped 1 data keys with vault-transit" || fail "rewrap: $(cat "$WORK/rewrap.out")"
keys "${VAULT_ENV[@]}" AKILI_ENCRYPTION_KEY=$ENC_KEY "$WORK/akili" keys rotate | tee "$WORK/rotate.out" | grep >/dev/null "re-encrypted" || fail "rotate: $(cat "$WORK/rotate.out")"
keys "${VAULT_ENV[@]}" "$WORK/akili" keys status >"$WORK/status.out" 2>&1 || fail "keys status on Vault alone: $(cat "$WORK/status.out")"
grep -q "legacy" "$WORK/status.out" && fail "legacy secrets remain: $(cat "$WORK/status.out")"
[ "$(grep -c "wrapped by vault-transit" "$WORK/status.out")" = "2" ] || fail "data keys not all on Vault: $(cat "$WORK/status.out")"
docker exec akili-h-pg psql -U akili -tAc "select token_enc from integrations where id='$INT'" | grep >/dev/null "^v2:" || fail "the secret was not re-encrypted"
docker exec akili-h-pg psql -U akili -tAc "select value from settings where key like 'crypto.dek.%'" | grep -v >/dev/null "^vault-transit:vault:v1:" && fail "a data key is not wrapped by Vault"
start_server env "${VAULT_ENV[@]}" AKILI_ENCRYPTION_KEY=
api POST /auth/login "{\"email\":\"admin@e2e.local\",\"password\":\"$PASS\"}" >/dev/null
[ "$(api GET /security | json "d['data']['kms']")" = "vault-transit" ] || fail "not running on Vault"
[ "$(api POST /integrations/$INT/test | json "d['data']['ok']")" = "True" ] || fail "cannot decrypt the integration secret with Vault alone"
wait_for "agent back online" 60 online

step "Forwarded headers are ignored without trusted proxies (rate limits cannot be dodged)"
codes=""
for i in $(seq 1 12); do
  codes+="$("${CURL[@]}" -o /dev/null -w '%{http_code}' -H "X-Forwarded-For: 10.9.8.$i" -H 'Content-Type: application/json' \
    -d '{"email":"nobody@example.com","password":"wrong-password-000"}' "$API/auth/login") "
done
[[ "$codes" == *429* ]] || fail "rotating X-Forwarded-For bypassed the login limit: $codes"
[ "$(docker exec akili-h-redis redis-cli --scan --pattern 'akili:rl:login:10.9.8.*' | wc -l | tr -d ' ')" = "0" ] || fail "a forged client IP was used as the rate-limit key"
stop_server
if keys "${VAULT_ENV[@]}" AKILI_VAULT_TOKEN=wrong-token "$WORK/akili" keys status >/dev/null 2>&1; then fail "a wrong Vault token opened the keyring"; fi

printf '\n\033[32mE2E HARDENING PASSED\033[0m\n'
