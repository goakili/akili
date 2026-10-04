#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Jonas Kaninda
# SPDX-License-Identifier: AGPL-3.0-or-later
# End-to-end test of coding work against a real GitLab CE: project and group access tokens, a
# nested-group project, a task that writes, tests in the sandbox, commits, pushes and opens a merge
# request, commit statuses, the diff, the push guard (Akili's and GitLab's own), repository creation
# with a group token, and a real issue webhook. Uses the scripted provider; needs Docker and about
# 4 GB of memory for GitLab, which takes a few minutes to boot. Opt-in: not part of e2e-all.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
AGENT_DIR="${AGENT_DIR:-$ROOT/agent}"
WORK="$(mktemp -d)"
PG_PORT=${PG_PORT:-55482}
REDIS_PORT=${REDIS_PORT:-56429}
GL_PORT=${GL_PORT:-58929}
GL_IMAGE=${GL_IMAGE:-gitlab/gitlab-ce:18.4.1-ce.0}
PORT=${PORT:-18390}
BASE="http://127.0.0.1:$PORT"
API="$BASE/api/v1"
GL="http://127.0.0.1:$GL_PORT"
JAR="$WORK/cookies"
PASS="e2e-password-123456"
ROOT_TOKEN="glpat-e2e-root-0123456789abcdef"
# GitLab refuses common words in passwords; the script signs in with ROOT_TOKEN, never with this.
ROOT_PASSWORD="$(openssl rand -hex 16)Zq"
SERVER_PID=""; AGENT_PID=""

cleanup() {
  [ -n "$AGENT_PID" ] && kill "$AGENT_PID" 2>/dev/null || true
  [ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null || true
  docker rm -f akili-gl-pg akili-gl-redis >/dev/null 2>&1 || true
  [ "${KEEP_GITLAB:-}" = "1" ] || docker rm -f akili-gl-gitlab >/dev/null 2>&1 || true
  docker ps -aq --filter name=akili-sbx- | xargs -r docker rm -f >/dev/null 2>&1 || true
  if [ "${KEEP:-}" = "1" ]; then echo "logs kept in $WORK"; else rm -rf "$WORK"; fi
}
trap cleanup EXIT

step() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }
fail() { printf '\033[31mFAIL: %s\033[0m\n' "$*"; echo "--- server log"; tail -40 "$WORK/server.log" || true; echo "--- agent log"; tail -40 "$WORK/agent.log" || true; KEEP=1; exit 1; }
json() { python3 -c "import json,sys; d=json.load(sys.stdin); print(eval(sys.argv[1], {'d': d}))" "$1"; }
enc() { python3 -c 'import sys,urllib.parse;print(urllib.parse.quote(sys.argv[1],safe=""))' "$1"; }
api() {
  local m=$1 p=$2 b=${3:-}
  if [ -n "$b" ]; then curl -sS -b "$JAR" -c "$JAR" -X "$m" -H 'Content-Type: application/json' -d "$b" "$API$p"
  else curl -sS -b "$JAR" -c "$JAR" -X "$m" "$API$p"; fi
}
gl() { # method path [json body]
  local m=$1 p=$2 b=${3:-}
  if [ -n "$b" ]; then curl -sS -X "$m" -H "PRIVATE-TOKEN: $ROOT_TOKEN" -H 'Content-Type: application/json' -d "$b" "$GL/api/v4$p"
  else curl -sS -X "$m" -H "PRIVATE-TOKEN: $ROOT_TOKEN" "$GL/api/v4$p"; fi
}
gl_code() { curl -s -o /dev/null -w '%{http_code}' -H "PRIVATE-TOKEN: $ROOT_TOKEN" "$GL/api/v4$1"; }
wait_for() {
  local desc=$1 t=$2; shift 2
  for _ in $(seq "$t"); do if "$@" >/dev/null 2>&1; then return 0; fi; sleep 1; done
  fail "timed out waiting for $desc"
}
# A fresh top-level group per run, so a kept GitLab container (KEEP_GITLAB=1) can be reused.
TOPG="platform-$(date +%s)"
NS="$TOPG/backend"
EXPIRES=$(python3 -c 'import datetime;print((datetime.date.today()+datetime.timedelta(days=30)).isoformat())')

step "Starting Postgres, Redis and GitLab CE (GitLab takes a few minutes)"
docker rm -f akili-gl-pg akili-gl-redis >/dev/null 2>&1 || true
docker run -d --name akili-gl-pg -e POSTGRES_USER=akili -e POSTGRES_PASSWORD=akili -e POSTGRES_DB=akili -p "$PG_PORT:5432" postgres:17-alpine >/dev/null
docker run -d --name akili-gl-redis -p "$REDIS_PORT:6379" redis:7-alpine >/dev/null
if ! docker ps --format '{{.Names}}' | grep -qx akili-gl-gitlab; then
  docker rm -f akili-gl-gitlab >/dev/null 2>&1 || true
  docker run -d --name akili-gl-gitlab --shm-size 256m --add-host=host.docker.internal:host-gateway -p "$GL_PORT:$GL_PORT" \
    -e GITLAB_OMNIBUS_CONFIG="external_url '$GL'; prometheus_monitoring['enable'] = false; registry['enable'] = false; gitlab_kas['enable'] = false; puma['worker_processes'] = 0; sidekiq['concurrency'] = 5; gitlab_rails['initial_root_password'] = '$ROOT_PASSWORD'" \
    "$GL_IMAGE" >/dev/null
fi
wait_for "postgres" 60 docker exec akili-gl-pg pg_isready -U akili
gl_ready() { curl -fsS -o /dev/null "$GL/users/sign_in"; }
wait_for "gitlab" 900 gl_ready
# A root token with a known value; set_token keeps the script free of output parsing.
docker exec akili-gl-gitlab gitlab-rails runner "
u = User.find_by_username('root')
u.personal_access_tokens.find_by(name: 'akili-e2e')&.destroy
t = u.personal_access_tokens.build(name: 'akili-e2e', scopes: ['api', 'admin_mode'], expires_at: 30.days.from_now)
t.set_token('$ROOT_TOKEN'); t.save!
" >/dev/null
[ "$(gl GET /user | json "d['username']")" = "root" ] || fail "no GitLab root token"
# GitLab refuses webhooks to private addresses by default; the e2e control plane runs on the host.
gl PUT "/application/settings?allow_local_requests_from_web_hooks_and_services=true" >/dev/null

step "Nested groups, a project, and project and group access tokens"
TOP=$(gl POST /groups "{\"name\":\"$TOPG\",\"path\":\"$TOPG\",\"visibility\":\"private\"}" | json "d['id']")
SUB=$(gl POST /groups "{\"name\":\"backend\",\"path\":\"backend\",\"parent_id\":$TOP,\"visibility\":\"private\"}" | json "d['id']")
GLP=$(gl POST /projects "{\"name\":\"inventory-api\",\"path\":\"inventory-api\",\"namespace_id\":$SUB,\"initialize_with_readme\":true,\"default_branch\":\"main\"}" | json "d['id']")
[ "$(gl GET /projects/$GLP | json "d['path_with_namespace']")" = "$NS/inventory-api" ] || fail "gitlab project"
PTOKEN=$(gl POST /projects/$GLP/access_tokens "{\"name\":\"akili\",\"scopes\":[\"api\"],\"access_level\":30,\"expires_at\":\"$EXPIRES\"}" | json "d['token']")
OTOKEN=$(gl POST /projects/$GLP/access_tokens "{\"name\":\"akili-owner\",\"scopes\":[\"api\"],\"access_level\":50,\"expires_at\":\"$EXPIRES\"}" | json "d['token']")
GTOKEN=$(gl POST /groups/$TOP/access_tokens "{\"name\":\"akili-group\",\"scopes\":[\"api\"],\"access_level\":40,\"expires_at\":\"$EXPIRES\"}" | json "d['token']")
for t in "$PTOKEN" "$OTOKEN" "$GTOKEN"; do case "$t" in glpat-*) ;; *) fail "no access token: $t";; esac; done

step "Building and starting the control plane"
(cd "$ROOT/server" && go build -o "$WORK/akili" ./cmd/akili)
(cd "$AGENT_DIR" && go build -o "$WORK/akili-agent" ./cmd/akili-agent)
AKILI_ENV=development AKILI_PORT=$PORT AKILI_PUBLIC_URL=$BASE AKILI_ENV_FILE=/dev/null \
AKILI_DATABASE_URL="postgres://akili:akili@127.0.0.1:$PG_PORT/akili?sslmode=disable" \
AKILI_REDIS_ADDR="127.0.0.1:$REDIS_PORT" AKILI_ADMIN_EMAIL=admin@e2e.local AKILI_ADMIN_PASSWORD=$PASS \
ANTHROPIC_API_KEY= "$WORK/akili" server >"$WORK/server.log" 2>&1 &
SERVER_PID=$!
wait_for "control plane" 60 curl -fsS "$BASE/healthz"
api POST /auth/login "{\"email\":\"admin@e2e.local\",\"password\":\"$PASS\"}" >/dev/null

step "Agent with the developer policy at L2"
POLICY=$(api GET /policies | json "[p['id'] for p in d['data'] if p['name']=='developer'][0]")
created=$(api POST /agents "{\"name\":\"coder-1\",\"labels\":[\"coder\"],\"policy_id\":\"$POLICY\",\"autonomy\":2}")
AGENT=$(echo "$created" | json "d['data']['agent']['id']")
TOKEN=$(echo "$created" | json "d['data']['join_token']")
mkdir -p "$WORK/st" "$WORK/wk"
AKILI_JOIN_TOKEN=$TOKEN AKILI_LOG_FORMAT=text "$WORK/akili-agent" enroll --url "$BASE" --state-dir "$WORK/st" --workdir "$WORK/wk" >>"$WORK/agent.log" 2>&1
AKILI_LOG_FORMAT=text "$WORK/akili-agent" start --state-dir "$WORK/st" >>"$WORK/agent.log" 2>&1 &
AGENT_PID=$!
online() { [ "$(api GET /agents/$AGENT | json "d['data']['status']")" = "online" ]; }
wait_for "agent online" 30 online
api PATCH /agents/$AGENT '{"git_name":"Coder One","git_email":"coder-1@agents.e2e.local"}' | grep >/dev/null '"success":true' || fail "agent git identity"

step "GitLab integration with a project access token: kind and expiry are read"
INT=$(api POST /integrations "{\"name\":\"gitlab\",\"kind\":\"gitlab\",\"base_url\":\"$GL/api/v4\",\"token\":\"$PTOKEN\",\"webhook_secret\":\"hook-secret\"}" | json "d['data']['id']")
TEST=$(api POST /integrations/$INT/test)
[ "$(echo "$TEST" | json "d['data']['ok']")" = "True" ] || fail "integration test: $TEST"
echo "$TEST" | json "d['data']['reply']" | grep >/dev/null "project token, expires $EXPIRES" || fail "token kind and expiry not reported: $TEST"
IT=$(api GET /integrations | json "[i['base_url'] + ' ' + i['token_kind'] + ' ' + i['token_expires_at'][:10] for i in d['data'] if i['id']=='$INT'][0]")
[ "$IT" = "$GL project $EXPIRES" ] || fail "integration fields: $IT"
api GET /integrations | grep >/dev/null "$PTOKEN" && fail "the token was returned by the API"

step "Projects: nested-group owner, project token can't create, Owner-role token refused"
PROJ=$(api POST /projects "{\"integration_id\":\"$INT\",\"owner\":\"$NS\",\"repo\":\"inventory-api\",\"agent_id\":\"$AGENT\",\"sandbox_image\":\"alpine:3\",\"trigger_label\":\"akili\"}" | json "d['data']['project']['id']")
[ "$(api GET /projects/$PROJ | json "d['data']['owner'] + ' ' + d['data']['forge']")" = "$NS gitlab" ] || fail "project owner/forge"
api POST /projects "{\"integration_id\":\"$INT\",\"owner\":\"$NS\",\"repo\":\"new-one\",\"create_repo\":true}" | grep >/dev/null "project access token can't create projects" || fail "a project token was used to create a repository"
api POST /projects "{\"integration_id\":\"$INT\",\"owner\":\"$TOPG//backend\",\"repo\":\"inventory-api\"}" | grep >/dev/null '"success":false' || fail "an empty group segment was accepted"
OINT=$(api POST /integrations "{\"name\":\"gitlab-owner\",\"kind\":\"gitlab\",\"base_url\":\"$GL\",\"token\":\"$OTOKEN\"}" | json "d['data']['id']")
api POST /projects "{\"integration_id\":\"$OINT\",\"owner\":\"$NS\",\"repo\":\"inventory-api\"}" | grep >/dev/null "Owner role" || fail "an Owner-role token was accepted"
for remote in "$GL/$NS/inventory-api.git" "git@127.0.0.1:$NS/inventory-api.git"; do
  [ "$(api GET "/projects/resolve?remote=$(enc "$remote")" | json "d['data']['id']")" = "$PROJ" ] || fail "remote $remote did not resolve to the project"
done

task_status() { api GET /tasks/$1 | json "d['data']['status']"; }
wait_task() {
  for _ in $(seq "$3"); do s=$(task_status "$1"); [ "$s" = "$2" ] && return 0
    case "$s" in succeeded|failed|cancelled|timed_out) fail "task $1 ended $s (wanted $2): $(api GET /tasks/$1)";; esac; sleep 1; done
  fail "task $1 stuck in $(task_status "$1")"
}
tool_out() { api GET /sessions/$1 | json "[e['payload'].get('output','') for e in d['data']['events'] if e['type']=='tool.result' and e['payload'].get('tool')=='$2']"; }

step "Coding task: write, test in the sandbox, commit, push, open a merge request"
GOAL='write: hello.txt :: hello from akili\nsandbox: cat hello.txt && echo SANDBOX_OK\ncommit: Add hello.txt\npush\npr: Add hello file\nchecks'
T1=$(api POST /tasks "{\"goal\":\"$GOAL\",\"project_id\":\"$PROJ\",\"autonomy\":2}" | json "d['data']['id']")
wait_task "$T1" succeeded 180
BRANCH=$(api GET /tasks/$T1 | json "d['data']['branch']")
[ "$BRANCH" = "akili/$T1" ] || fail "unexpected branch $BRANCH"
MRN=$(api GET /tasks/$T1 | json "d['data']['pr_number']")
[ "$MRN" -ge 1 ] || fail "no merge request recorded on the task"
MR=$(gl GET /projects/$GLP/merge_requests/$MRN)
[ "$(echo "$MR" | json "d['source_branch'] + ' ' + d['target_branch'] + ' ' + d['state']")" = "$BRANCH main opened" ] || fail "merge request: $MR"
echo "$MR" | json "d['description']" | grep >/dev/null "<!-- akili:task=$T1 agent=$AGENT -->" || fail "MR has no Akili marker: $MR"
[ "$(api GET /tasks/$T1 | json "d['data']['pr_url']")" = "$(echo "$MR" | json "d['web_url']")" ] || fail "task links another URL than the MR"
[ "$(gl_code "/projects/$GLP/repository/files/hello.txt?ref=$(enc "$BRANCH")")" = "200" ] || fail "hello.txt not on the branch"
[ "$(gl_code "/projects/$GLP/repository/files/hello.txt?ref=main")" = "404" ] || fail "main was modified"
HEAD_COMMIT=$(gl GET "/projects/$GLP/repository/commits/$(enc "$BRANCH")")
[ "$(echo "$HEAD_COMMIT" | json "d['author_email'] + ' ' + d['committer_email'] + ' ' + d['author_name']")" = "coder-1@agents.e2e.local coder-1@agents.e2e.local Coder One" ] || fail "commit identity: $HEAD_COMMIT"
echo "$HEAD_COMMIT" | json "d['message']" | grep >/dev/null "^Akili-Task: $T1$" || fail "commit has no Akili-Task trailer"
SES1=$(api GET /tasks/$T1 | json "d['data']['session_id']")
api GET /sessions/$SES1 | grep >/dev/null "SANDBOX_OK" || fail "sandbox output missing"
tool_out "$SES1" pr_open | grep >/dev/null "Opened MR !$MRN" || fail "pr_open did not name the merge request: $(tool_out "$SES1" pr_open)"
tool_out "$SES1" pr_status | grep >/dev/null "MR !$MRN" || fail "pr_status did not find the merge request: $(tool_out "$SES1" pr_status)"

step "CI: commit statuses on the branch head (latest per job, skipped left out)"
SHA=$(echo "$HEAD_COMMIT" | json "d['id']")
gl POST "/projects/$GLP/statuses/$SHA" '{"state":"failed","name":"ci/test"}' >/dev/null
gl POST "/projects/$GLP/statuses/$SHA" '{"state":"running","name":"ci/test"}' >/dev/null
gl POST "/projects/$GLP/statuses/$SHA" '{"state":"skipped","name":"ci/docs"}' >/dev/null
SES=$(api POST /sessions "{\"agent_id\":\"$AGENT\",\"project_id\":\"$PROJ\",\"title\":\"ci\"}" | json "d['data']['id']")
# A session gets its own branch: point it at the task's head so its checks read the statuses above.
gl POST "/projects/$GLP/repository/branches?branch=$(enc "akili/$SES")&ref=$SHA" >/dev/null
api POST "/sessions/$SES/messages" '{"text":"checks"}' >/dev/null
checked() { tool_out "$SES" pr_status | grep -q "CI:"; }
wait_for "pr_status in the session" 60 checked
OUT=$(tool_out "$SES" pr_status)
echo "$OUT" | grep >/dev/null "CI: pending" || fail "a running job did not make CI pending: $OUT"
echo "$OUT" | grep >/dev/null "ci/test: pending" || fail "the retried job did not report its latest state: $OUT"
echo "$OUT" | grep >/dev/null "ci/docs" && fail "a skipped job was counted: $OUT"

step "Merge request diff through the API"
curl -sS -b "$JAR" "$API/tasks/$T1/diff" | grep >/dev/null "+hello from akili" || fail "diff endpoint"

step "Push guard: Akili refuses a push to main, and GitLab refuses the Developer token on main"
api POST "/sessions/$SES/messages" '{"text":"write: evil.txt :: evil\ncommit: evil\nrun: git push origin HEAD:refs/heads/main"}' >/dev/null
pending() { [ "$(api GET '/approvals?status=pending' | json "len(d['data'])")" -ge 1 ]; }
wait_for "shell approval" 60 pending
api POST "/approvals/$(api GET '/approvals?status=pending' | json "d['data'][0]['id']")/approve" '{}' >/dev/null
refused() { api GET "/audit?action=git.push_refused" | json "d['data']['total']" | grep -v >/dev/null "^0$"; }
wait_for "refused push in the audit log" 60 refused
api GET "/audit?action=git.push_refused" | json "d['data']['items'][0]['metadata']['refs']" | grep >/dev/null "refs/heads/main" || fail "refused push not recorded"
[ "$(gl_code "/projects/$GLP/repository/files/evil.txt?ref=main")" = "404" ] || fail "a push to main got through Akili"
git clone -q "http://oauth2:$PTOKEN@127.0.0.1:$GL_PORT/$NS/inventory-api.git" "$WORK/direct" 2>/dev/null || fail "direct clone with the project token"
(cd "$WORK/direct" && echo direct >direct.txt && git add direct.txt && git -c user.name=t -c user.email=t@e2e.local commit -qm direct && ! git push -q origin HEAD:main 2>"$WORK/direct.err") || fail "GitLab let the Developer token push to the protected main branch"
grep -qi "protected" "$WORK/direct.err" || fail "GitLab refused for another reason: $(cat "$WORK/direct.err")"

step "Group access token (Maintainer) creates a repository in a nested group"
GINT=$(api POST /integrations "{\"name\":\"gitlab-group\",\"kind\":\"gitlab\",\"base_url\":\"$GL\",\"token\":\"$GTOKEN\"}" | json "d['data']['id']")
[ "$(api POST /integrations/$GINT/test | json "d['data']['ok']")" = "True" ] || fail "group token test"
R=$(api POST /projects "{\"integration_id\":\"$GINT\",\"owner\":\"$NS\",\"repo\":\"orders-api\",\"create_repo\":true,\"private\":true}")
[ "$(echo "$R" | json "d['data']['project']['owner']")" = "$NS" ] || fail "create repository: $R"
[ "$(gl GET "/projects/$(enc $NS/orders-api)" | json "d['default_branch'] + ' ' + d['visibility']")" = "main private" ] || fail "repository not created on GitLab"

step "Issue webhook: a real GitLab hook turns a labelled issue into a task; a wrong token is refused"
HOOK_URL="http://host.docker.internal:$PORT/api/v1/webhooks/forge/$INT"
gl POST /projects/$GLP/hooks "{\"url\":\"$HOOK_URL\",\"token\":\"hook-secret\",\"issues_events\":true,\"push_events\":false,\"enable_ssl_verification\":false}" | json "d['id']" >/dev/null
IID=$(gl POST /projects/$GLP/issues '{"title":"Add a /version endpoint","description":"Please add it.","labels":"akili"}' | json "d['iid']")
issue_task() { api GET /tasks | json "[t for t in d['data'] if t['trigger']=='issue' and t['trigger_ref']=='issue:$NS/inventory-api#$IID']" | grep -q "Add a /version endpoint"; }
wait_for "issue task from the GitLab webhook" 90 issue_task
bad=$(curl -sS -o /dev/null -w '%{http_code}' -H "X-Gitlab-Event: Issue Hook" -H "X-Gitlab-Token: wrong" -H 'Content-Type: application/json' -d '{}' "$API/webhooks/forge/$INT")
[ "$bad" = "401" ] || fail "a wrong webhook token returned $bad"
IID2=$(gl POST /projects/$GLP/issues '{"title":"Unlabelled","description":"no label"}' | json "d['iid']")
gl PUT "/projects/$GLP/issues/$IID2" '{"labels":"akili"}' >/dev/null
label_task() { api GET /tasks | json "[t for t in d['data'] if t['trigger_ref']=='issue:$NS/inventory-api#$IID2']" | grep -q "Unlabelled"; }
wait_for "task when the label is added later" 90 label_task

step "Audit chain verifies"
[ "$(api GET /audit/verify | json "d['data']['valid']")" = "True" ] || fail "audit chain invalid"

printf '\n\033[32mE2E GITLAB PASSED\033[0m\n'
