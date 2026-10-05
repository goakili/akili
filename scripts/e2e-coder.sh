#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Jonas Kaninda
# SPDX-License-Identifier: AGPL-3.0-or-later
# End-to-end test of coding work against a real Gitea: integration, project, a task that
# writes, tests in the sandbox, commits, pushes and opens a PR, the push guard, the agent's git
# identity, diffs, webhooks and templates. Uses the scripted provider; needs Docker.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# The agent is its own repository, checked out next to this one.
AGENT_DIR="${AGENT_DIR:-$ROOT/agent}"
WORK="$(mktemp -d)"
PG_PORT=${PG_PORT:-55472}
REDIS_PORT=${REDIS_PORT:-56419}
GITEA_PORT=${GITEA_PORT:-53000}
PORT=${PORT:-18380}
BASE="http://127.0.0.1:$PORT"
API="$BASE/api/v1"
GITEA="http://127.0.0.1:$GITEA_PORT"
JAR="$WORK/cookies"
PASS="e2e-password-123456"
GUSER=akili
GPASS="gitea-e2e-pass-123"
SERVER_PID=""; AGENT_PID=""

cleanup() {
  [ -n "$AGENT_PID" ] && kill "$AGENT_PID" 2>/dev/null || true
  [ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null || true
  docker rm -f akili-c-pg akili-c-redis akili-c-gitea >/dev/null 2>&1 || true
  docker ps -aq --filter name=akili-sbx- | xargs -r docker rm -f >/dev/null 2>&1 || true
  if [ "${KEEP:-}" = "1" ]; then echo "logs kept in $WORK"; else rm -rf "$WORK"; fi
}
trap cleanup EXIT

step() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }
fail() { printf '\033[31mFAIL: %s\033[0m\n' "$*"; echo "--- server log"; tail -40 "$WORK/server.log" || true; echo "--- agent log"; tail -40 "$WORK/agent.log" || true; KEEP=1; exit 1; }
json() { python3 -c "import json,sys; d=json.load(sys.stdin); print(eval(sys.argv[1], {'d': d}))" "$1"; }
api() {
  local m=$1 p=$2 b=${3:-}
  if [ -n "$b" ]; then curl -sS -b "$JAR" -c "$JAR" -X "$m" -H 'Content-Type: application/json' -d "$b" "$API$p"
  else curl -sS -b "$JAR" -c "$JAR" -X "$m" "$API$p"; fi
}
gitea() { curl -sS -u "$GUSER:$GPASS" "$GITEA/api/v1$1"; }
wait_for() {
  local desc=$1 t=$2; shift 2
  for _ in $(seq "$t"); do if "$@" >/dev/null 2>&1; then return 0; fi; sleep 1; done
  fail "timed out waiting for $desc"
}

step "Starting Postgres, Redis and Gitea"
docker rm -f akili-c-pg akili-c-redis akili-c-gitea >/dev/null 2>&1 || true
docker run -d --name akili-c-pg -e POSTGRES_USER=akili -e POSTGRES_PASSWORD=akili -e POSTGRES_DB=akili -p "$PG_PORT:5432" postgres:17-alpine >/dev/null
docker run -d --name akili-c-redis -p "$REDIS_PORT:6379" redis:7-alpine >/dev/null
docker run -d --name akili-c-gitea -e GITEA__security__INSTALL_LOCK=true -e GITEA__database__DB_TYPE=sqlite3 \
  -e GITEA__server__ROOT_URL="$GITEA/" -e GITEA__service__DISABLE_REGISTRATION=true -e GITEA__repository__DEFAULT_BRANCH=main \
  -p "$GITEA_PORT:3000" gitea/gitea:1.26 >/dev/null
wait_for "postgres" 60 docker exec akili-c-pg pg_isready -U akili
wait_for "gitea" 90 curl -fsS "$GITEA/api/healthz"
docker exec -u git akili-c-gitea gitea admin user create --admin --username "$GUSER" --password "$GPASS" --email akili@e2e.local --must-change-password=false >/dev/null
GTOKEN=$(curl -sS -u "$GUSER:$GPASS" -H 'Content-Type: application/json' -d '{"name":"akili-e2e","scopes":["all"]}' "$GITEA/api/v1/users/$GUSER/tokens" | json "d['sha1']")
[ -n "$GTOKEN" ] || fail "no gitea token"

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

step "Git identity: a member's email and a malformed one are refused, a bot email is kept"
patch_code() { curl -sS -o /dev/null -w '%{http_code}' -b "$JAR" -X PATCH -H 'Content-Type: application/json' -d "$1" "$API/agents/$AGENT"; }
[ "$(api GET /agents/$AGENT | json "d['data']['git_identity']['email']")" = "agent@goakili.dev" ] || fail "default git identity"
[ "$(patch_code '{"git_email":"Admin@e2e.local"}')" = "400" ] || fail "an agent was given a member's email"
[ "$(patch_code '{"git_email":"bot@e2e.local\n[core]"}')" = "400" ] || fail "an email with a newline was accepted"
[ "$(patch_code '{"git_name":"Coder One","git_email":"coder-1@agents.e2e.local"}')" = "200" ] || fail "setting the git identity"
[ "$(api GET /agents/$AGENT | json "d['data']['git_identity']['email']")" = "coder-1@agents.e2e.local" ] || fail "git identity not saved"
api GET "/audit?action=agent.update" | grep >/dev/null "coder-1@agents.e2e.local" || fail "git identity change not audited"

step "Requester credit: the admin opts in to a co-author trailer and a PR mention"
put_code() { curl -sS -o /dev/null -w '%{http_code}' -b "$JAR" -X PUT -H 'Content-Type: application/json' -d "$1" "$API/auth/git-identity"; }
[ "$(put_code '{"forge_login":"x-->"}')" = "400" ] || fail "a forge login that breaks markdown was accepted"
[ "$(put_code '{"co_author_email":"a@b.c>"}')" = "400" ] || fail "a malformed co-author email was accepted"
[ "$(put_code '{"forge_login":"@e2e-admin","co_author_email":"1+e2e-admin@users.noreply.github.com"}')" = "200" ] || fail "setting the co-author identity"
[ "$(api GET /auth/git-identity | json "d['data']['forge_login']")" = "e2e-admin" ] || fail "forge login not saved without its @"

step "Gitea integration"
INT=$(api POST /integrations "{\"name\":\"gitea\",\"kind\":\"gitea\",\"base_url\":\"$GITEA\",\"username\":\"$GUSER\",\"token\":\"$GTOKEN\",\"webhook_secret\":\"hook-secret\"}" | json "d['data']['id']")
[ "$(api POST /integrations/$INT/test | json "d['data']['ok']")" = "True" ] || fail "integration test failed"
api GET /integrations | grep >/dev/null "$GTOKEN" && fail "an integration secret was returned by the API"

step "Project with a new repository and a sandbox image"
PROJ=$(api POST /projects "{\"integration_id\":\"$INT\",\"owner\":\"$GUSER\",\"repo\":\"demo-svc\",\"create_repo\":true,\"agent_id\":\"$AGENT\",\"sandbox_image\":\"alpine:3\",\"trigger_label\":\"akili\"}" | json "d['data']['project']['id']")
[ "$(gitea /repos/$GUSER/demo-svc | json "d['default_branch']")" = "main" ] || fail "repository not created on gitea"

task_status() { api GET /tasks/$1 | json "d['data']['status']"; }
wait_task() {
  for _ in $(seq "$3"); do s=$(task_status "$1"); [ "$s" = "$2" ] && return 0
    case "$s" in succeeded|failed|cancelled|timed_out) fail "task $1 ended $s (wanted $2): $(api GET /tasks/$1)";; esac; sleep 1; done
  fail "task $1 stuck in $(task_status "$1")"
}

step "Editor project lookup: a git remote finds its project, in https, ssh and scp form"
for remote in "$GITEA/$GUSER/demo-svc.git" "git@127.0.0.1:$GUSER/demo-svc.git" "ssh://git@127.0.0.1:2222/$GUSER/Demo-Svc"; do
  [ "$(api GET "/projects/resolve?remote=$(python3 -c 'import sys,urllib.parse;print(urllib.parse.quote(sys.argv[1],safe=""))' "$remote")" | json "d['data']['id']")" = "$PROJ" ] || fail "remote $remote did not resolve to the project"
done
[ "$(curl -sS -o /dev/null -w '%{http_code}' -b "$JAR" "$API/projects/resolve?remote=https%3A%2F%2Fgithub.com%2F$GUSER%2Fdemo-svc")" = "404" ] || fail "a remote on another host resolved to the project"
[ "$(curl -sS -o /dev/null -w '%{http_code}' -b "$JAR" "$API/projects/resolve?remote=%2Fhome%2Fme%2Frepo")" = "400" ] || fail "a local path was accepted as a remote"

step "Coding task: write, test in the sandbox, commit, push, open a PR, check CI"
GOAL='write: hello.txt :: hello from akili\nsandbox: cat hello.txt && echo SANDBOX_OK && echo SBX_UID=$(id -u)\ncommit: Add hello.txt\npush\npr: Add hello file\nchecks'
T1=$(api POST /tasks "{\"goal\":\"$GOAL\",\"project_id\":\"$PROJ\",\"autonomy\":2}" | json "d['data']['id']")
wait_task "$T1" succeeded 120
BRANCH=$(api GET /tasks/$T1 | json "d['data']['branch']")
[ "$BRANCH" = "akili/$T1" ] || fail "unexpected branch $BRANCH"
PRN=$(api GET /tasks/$T1 | json "d['data']['pr_number']")
[ "$PRN" -ge 1 ] || fail "no PR recorded on the task"
[ "$(gitea /repos/$GUSER/demo-svc/pulls/$PRN | json "d['head']['ref']")" = "$BRANCH" ] || fail "PR head is not the task branch"
gitea "/repos/$GUSER/demo-svc/contents/hello.txt?ref=$BRANCH" | json "d['name']" | grep >/dev/null hello.txt || fail "hello.txt not on the branch"
[ "$(curl -s -o /dev/null -w '%{http_code}' -u "$GUSER:$GPASS" "$GITEA/api/v1/repos/$GUSER/demo-svc/contents/hello.txt?ref=main")" = "404" ] || fail "main was modified"
HEAD_COMMIT=$(gitea "/repos/$GUSER/demo-svc/commits?sha=$BRANCH&limit=1")
[ "$(echo "$HEAD_COMMIT" | json "d[0]['commit']['author']['email'] + ' ' + d[0]['commit']['committer']['email']")" = "coder-1@agents.e2e.local coder-1@agents.e2e.local" ] || fail "commit not made with the agent's identity: $HEAD_COMMIT"
[ "$(echo "$HEAD_COMMIT" | json "d[0]['commit']['author']['name']")" = "Coder One" ] || fail "commit author name"
echo "$HEAD_COMMIT" | json "d[0]['commit']['message']" | grep >/dev/null "^Akili-Task: $T1$" || fail "commit has no Akili-Task trailer: $HEAD_COMMIT"
echo "$HEAD_COMMIT" | json "d[0]['commit']['message']" | grep >/dev/null "^Akili-Agent: coder-1$" || fail "commit has no Akili-Agent trailer: $HEAD_COMMIT"
echo "$HEAD_COMMIT" | json "d[0]['commit']['message']" | grep >/dev/null "^Co-Authored-By: .* <1+e2e-admin@users.noreply.github.com>$" || fail "commit does not credit the requester: $HEAD_COMMIT"
PR_BODY=$(gitea /repos/$GUSER/demo-svc/pulls/$PRN | json "d['body']")
echo "$PR_BODY" | grep >/dev/null "requested by @e2e-admin · task \`$T1\`" || fail "PR footer does not credit the requester: $PR_BODY"
echo "$PR_BODY" | grep >/dev/null "<!-- akili:task=$T1 agent=$AGENT -->" || fail "PR footer has no marker: $PR_BODY"
SES1=$(api GET /tasks/$T1 | json "d['data']['session_id']")
api GET /sessions/$SES1 | grep >/dev/null "SANDBOX_OK" || fail "sandbox output missing"
api GET /sessions/$SES1 | json "[e['payload'].get('output','') for e in d['data']['events'] if e['type']=='tool.result' and e['payload'].get('tool')=='sandbox_exec'][0]" | grep >/dev/null "SBX_UID=$(id -u)$" || fail "sandbox did not run as the agent's user"
api GET /sessions/$SES1 | json "[e['payload'].get('output','') for e in d['data']['events'] if e['type']=='tool.result' and e['payload'].get('tool')=='pr_open'][0]" | grep >/dev/null "Opened PR #$PRN" || fail "pr_open result missing"

step "Project plans: a draft task linked to a plan, started, reports progress with plan_phase_update"
PLAN=$(api POST /projects/$PROJ/plans '{"title":"Version endpoint","description":"Expose the build version.","phases":[{"title":"Add /version"},{"title":"Document it","done_when":"the README shows an example"}]}')
PLN=$(echo "$PLAN" | json "d['data']['plan']['id']")
PHS1=$(echo "$PLAN" | json "d['data']['phases'][0]['id']")
PHS2=$(echo "$PLAN" | json "d['data']['phases'][1]['id']")
[ "$(echo "$PLAN" | json "d['data']['plan']['status']")" = "active" ] || fail "a new plan is not active: $PLAN"
DRAFT_PLN=$(api POST /projects/$PROJ/plans '{"title":"Later","status":"draft"}' | json "d['data']['plan']['id']")
link_code() { curl -sS -o /dev/null -w '%{http_code}' -b "$JAR" -X POST -H 'Content-Type: application/json' -d "$1" "$API/tasks"; }
# Bodies go through variables: bash 3.2 brace-expands a quoted JSON string inside "$(...)".
BODY='{"goal":"x","project_id":"'$PROJ'","plan_ids":["'$DRAFT_PLN'"]}'
[ "$(link_code "$BODY")" = "400" ] || fail "a draft plan was linked to a task"
BODY='{"goal":"x","plan_ids":["'$PLN'"]}'
[ "$(link_code "$BODY")" = "400" ] || fail "a plan was linked to a task without a project"
PGOAL="tool: plan_phase_update {\\\"plan\\\":\\\"$PLN\\\",\\\"phase\\\":\\\"$PHS1\\\",\\\"status\\\":\\\"done\\\",\\\"note\\\":\\\"added with a test\\\"}"
TP=$(api POST /tasks "{\"goal\":\"$PGOAL\",\"project_id\":\"$PROJ\",\"plan_ids\":[\"$PLN\"],\"draft\":true}" | json "d['data']['id']")
[ -n "$TP" ] || fail "draft task not created"
sleep 3
[ "$(task_status "$TP")" = "draft" ] || fail "a draft task was dispatched"
[ "$(api GET /tasks/$TP | json "d['data']['plan_ids'][0]")" = "$PLN" ] || fail "the task does not list its plan"
[ "$(curl -sS -o /dev/null -w '%{http_code}' -b "$JAR" -X DELETE "$API/plans/$PLN")" = "409" ] || fail "a plan was deleted while an unfinished task works on it"
[ "$(api POST /tasks/$TP/start | json "d['data']['status']")" = "queued" ] || fail "starting the draft task"
[ "$(curl -sS -o /dev/null -w '%{http_code}' -b "$JAR" -X POST "$API/tasks/$TP/start")" = "409" ] || fail "a queued task was started twice"
wait_task "$TP" succeeded 120
PHASE=$(api GET /plans/$PLN | json "[s for s in d['data']['phases'] if s['id']=='$PHS1'][0]")
echo "$PHASE" | grep >/dev/null "'status': 'done'" || fail "the agent's phase update was not stored: $PHASE"
echo "$PHASE" | grep >/dev/null "'done_by_task': '$TP'" || fail "the phase does not record its task: $PHASE"
echo "$PHASE" | grep >/dev/null "'updated_by': 'agent:$AGENT'" || fail "the phase is not marked as changed by the agent: $PHASE"
[ "$(api GET /plans/$PLN | json "d['data']['plan']['status']")" = "in_progress" ] || fail "the plan did not follow its phases"
SESP=$(api GET /tasks/$TP | json "d['data']['session_id']")
api GET /sessions/$SESP | json "d['data']['messages'][0]['content'][0]['text']" | grep >/dev/null "\[plan $PLN\]" || fail "the agent did not receive the plan in its goal"
api GET "/audit?action=plan.phase_update" | grep >/dev/null "$PHS1" || fail "the agent's phase update was not audited"
[ "$(api GET /tasks/$TP/plans | json "d['data'][0]['snapshot']['phases'][0]['status']")" = "todo" ] || fail "the task's plan snapshot changed with the plan"

TQ=$(api POST /tasks "{\"goal\":\"$PGOAL\",\"project_id\":\"$PROJ\"}" | json "d['data']['id']")
wait_task "$TQ" succeeded 120
SESQ=$(api GET /tasks/$TQ | json "d['data']['session_id']")
api GET /sessions/$SESQ | json "[e['payload'].get('output','') for e in d['data']['events'] if e['type']=='tool.result' and e['payload'].get('tool')=='plan_phase_update'][0]" | grep >/dev/null "not linked to this task" || fail "a task changed a plan it is not linked to"

# One task per phase: the agent sees the whole plan but may report on its own phase only.
BODY='{"goal":"x","project_id":"'$PROJ'","plan_phase_id":"'$PHS1'"}'
[ "$(link_code "$BODY")" = "400" ] || fail "a task was focused on a phase that is already done"
FGOAL='tool: plan_phase_update {\"plan\":\"'$PLN'\",\"phase\":\"'$PHS1'\",\"status\":\"in_progress\"}\ntool: plan_phase_update {\"plan\":\"'$PLN'\",\"phase\":\"'$PHS2'\",\"status\":\"in_progress\"}'
BODY='{"goal":"'$FGOAL'","project_id":"'$PROJ'","plan_phase_id":"'$PHS2'"}'
TF=$(api POST /tasks "$BODY" | json "d['data']['id']")
[ -n "$TF" ] || fail "creating a task focused on a phase"
[ "$(api GET /tasks/$TF/plans | json "d['data'][0]['phase_id']")" = "$PHS2" ] || fail "the task is not focused on its phase"
wait_task "$TF" succeeded 120
SESF=$(api GET /tasks/$TF | json "d['data']['session_id']")
GOALF=$(api GET /sessions/$SESF | json "d['data']['messages'][0]['content'][0]['text']")
echo "$GOALF" | grep >/dev/null "Document it ← this task" || fail "the focused phase is not marked in the goal: $GOALF"
echo "$GOALF" | grep >/dev/null "Done when: the README shows an example" || fail "the goal lacks the phase's done-when: $GOALF"
api GET /sessions/$SESF | json "[e['payload'].get('output','') for e in d['data']['events'] if e['type']=='tool.result' and e['payload'].get('tool')=='plan_phase_update'][0]" | grep >/dev/null "works only on phase $PHS2" || fail "a focused task changed another phase"
[ "$(api GET /plans/$PLN | json "[p['status'] for p in d['data']['phases'] if p['id']=='$PHS1'][0] + ' ' + [p['status'] for p in d['data']['phases'] if p['id']=='$PHS2'][0]")" = "done in_progress" ] || fail "phase statuses after the focused task: $(api GET /plans/$PLN)"
[ "$(api GET /plans/$PLN | json "[l['phase_id'] for l in d['data']['links'] if l['task_id']=='$TF'][0]")" = "$PHS2" ] || fail "the plan does not link the task to its phase"

KEPT=$(api PUT /plans/$PLN/phases "{\"phases\":[{\"id\":\"$PHS1\",\"title\":\"Add /version endpoint\"},{\"title\":\"Release it\"}]}")
[ "$(echo "$KEPT" | json "len(d['data']['phases'])")" = "2" ] || fail "replacing phases: $KEPT"
[ "$(echo "$KEPT" | json "d['data']['phases'][0]['status'] + ' ' + d['data']['phases'][0]['title']")" = "done Add /version endpoint" ] || fail "a kept phase lost its status: $KEPT"
echo "$KEPT" | json "[p['id'] for p in d['data']['phases']]" | grep >/dev/null "$PHS2" && fail "a removed phase is still there: $KEPT"
api PATCH /plans/$PLN/phases/$(echo "$KEPT" | json "d['data']['phases'][1]['id']") '{"status":"skipped","note":"not needed"}' >/dev/null
[ "$(api GET /plans/$PLN | json "d['data']['plan']['status']")" = "done" ] || fail "a plan with every phase done or skipped is not done"
[ "$(api GET /projects/$PROJ/plans | json "[p for p in d['data'] if p['id']=='$PLN'][0]['tasks']")" = "2" ] || fail "the plan list does not count its linked tasks"

step "plan_propose: an agent's plan arrives as a draft a person activates; a forged status is refused"
SESPP=$(api POST /sessions "{\"agent_id\":\"$AGENT\",\"project_id\":\"$PROJ\",\"title\":\"planning\"}" | json "d['data']['id']")
PROPOSE='tool: plan_propose {"title":"Health endpoint","description":"Add a /healthz that checks the database.","phases":[{"title":"Add /healthz","done_when":"it returns 200 when the database is up"},{"title":"Wire it into the Dockerfile HEALTHCHECK"}]}'
FORGED='tool: plan_propose {"title":"Skip review","status":"active","phases":[{"title":"x"}]}'
say() { api POST "/sessions/$SESPP/messages" "$(python3 -c 'import json,sys; print(json.dumps({"text": sys.argv[1]}))' "$1")" >/dev/null; }
proposals() { api GET "/sessions/$SESPP" | json "[e['payload'] for e in d['data']['events'] if e['type']=='tool.result' and e['payload'].get('tool')=='plan_propose']"; }
say "$PROPOSE"
proposed() { proposals | grep -q "Proposed plan"; }
wait_for "plan_propose result" 60 proposed
PROP_ID=$(api GET /projects/$PROJ/plans | json "[p['id'] for p in d['data'] if p['title']=='Health endpoint'][0]")
[ "$(api GET /plans/$PROP_ID | json "d['data']['plan']['status'] + ' ' + d['data']['plan']['created_by'] + ' ' + d['data']['plan']['proposed_session_id'] + ' ' + str(len(d['data']['phases']))")" = "draft agent:$AGENT $SESPP 2" ] || fail "proposed plan: $(api GET /plans/$PROP_ID)"
BODY='{"goal":"x","project_id":"'$PROJ'","plan_ids":["'$PROP_ID'"]}'
[ "$(link_code "$BODY")" = "400" ] || fail "a task was linked to an agent's proposal before it was activated"
api GET "/audit?action=plan.propose" | grep >/dev/null "$PROP_ID" || fail "the proposal was not audited"
say "$FORGED"
forged() { api GET "/sessions/$SESPP" | json "[e['payload'].get('effect') for e in d['data']['events'] if e['type']=='tool.request' and e['payload'].get('tool')=='plan_propose']" | grep -q deny; }
wait_for "forged plan_propose result" 60 forged
[ "$(api GET /projects/$PROJ/plans | json "len([p for p in d['data'] if p['title']=='Skip review'])")" = "0" ] || fail "a proposal with a forged status was stored"
[ "$(api PUT /plans/$PROP_ID '{"status":"active"}' | json "d['data']['status']")" = "active" ] || fail "activating the proposal"
[ "$(link_code "$BODY")" = "201" ] || fail "an activated proposal can't be linked to a task"

step "Large push: a 2 MB commit goes through both proxies (git's probe request, chunked body, identity check)"
GOAL='sandbox: head -c 1500000 /dev/urandom | base64 > big.txt && wc -c big.txt\ncommit: Add big.txt\npush'
TB=$(api POST /tasks "{\"title\":\"large push\",\"goal\":\"$GOAL\",\"project_id\":\"$PROJ\",\"autonomy\":2}" | json "d['data']['id']")
wait_task "$TB" succeeded 180
SIZE=$(gitea "/repos/$GUSER/demo-svc/contents/big.txt?ref=akili/$TB" | json "d.get('size', 0)")
[ "$SIZE" -gt 1500000 ] || fail "big.txt did not reach the forge intact (size $SIZE): $(api GET /tasks/$TB | json "d['data'].get('result','')")"
api GET "/audit?action=git.push_refused" | grep >/dev/null "empty push" && fail "git's probe request was refused as an empty push"

step "PR diff through the API"
curl -sS -b "$JAR" "$API/tasks/$T1/diff" | grep >/dev/null "+hello from akili" || fail "diff endpoint"

step "Push guard: pushing to main through the proxy is refused (after approval of the shell call)"
SES=$(api POST /sessions "{\"agent_id\":\"$AGENT\",\"project_id\":\"$PROJ\",\"title\":\"guard\"}" | json "d['data']['id']")
api POST "/sessions/$SES/messages" '{"text":"write: evil.txt :: evil\ncommit: evil\nrun: git push origin HEAD:refs/heads/main"}' >/dev/null
pending() { [ "$(api GET '/approvals?status=pending' | json "len(d['data'])")" -ge 1 ]; }
wait_for "shell approval" 60 pending
APPROVAL=$(api GET '/approvals?status=pending' | json "d['data'][0]['id']")
api POST "/approvals/$APPROVAL/approve" '{}' >/dev/null
refused() { api GET "/audit?action=git.push_refused" | json "(d.get('pageable') or {}).get('total_elements', len(d['data']))" | grep -v >/dev/null "^0$"; }
wait_for "refused push in the audit log" 60 refused
api GET "/audit?action=git.push_refused" | json "d['data'][0]['metadata']['refs']" | grep >/dev/null "refs/heads/main" || fail "refused push not recorded"
[ "$(curl -s -o /dev/null -w '%{http_code}' -u "$GUSER:$GPASS" "$GITEA/api/v1/repos/$GUSER/demo-svc/contents/evil.txt?ref=main")" = "404" ] || fail "a push to main got through"
shell_out() { api GET "/sessions/$SES" | json "[e['payload'].get('output','') for e in d['data']['events'] if e['type']=='tool.result' and e['payload'].get('tool')=='shell']"; }
OUT=""
for _ in $(seq 30); do OUT=$(shell_out); [ "$OUT" != "[]" ] && break; sleep 1; done
echo "$OUT" | grep >/dev/null "akili: push to refs/heads/main refused" || fail "git did not show the refusal to the agent: $OUT"
api GET "/audit?action=git.push" | json "(d.get('pageable') or {}).get('total_elements', len(d['data']))" | grep -v >/dev/null "^0$" || fail "allowed push not audited"

step "Push guard: a commit with a spoofed author is refused, even on the session's own branch"
SPOOF="write: spoof.txt :: spoof\nrun: git add -A \u0026\u0026 git -c user.name=CEO -c user.email=ceo@e2e.local commit -qm spoof \u0026\u0026 git push origin HEAD:refs/heads/akili/$SES"
api POST "/sessions/$SES/messages" "{\"text\":\"$SPOOF\"}" >/dev/null
wait_for "approval of the spoofing shell call" 60 pending
APPROVAL=$(api GET '/approvals?status=pending' | json "d['data'][0]['id']")
api POST "/approvals/$APPROVAL/approve" '{}' >/dev/null
spoof_refused() { api GET "/audit?action=git.push_refused" | grep >/dev/null "ceo@e2e.local"; }
wait_for "spoofed push refused in the audit log" 60 spoof_refused
[ "$(curl -s -o /dev/null -w '%{http_code}' -u "$GUSER:$GPASS" "$GITEA/api/v1/repos/$GUSER/demo-svc/contents/spoof.txt?ref=akili/$SES")" = "404" ] || fail "a spoofed commit reached the forge"
for _ in $(seq 30); do OUT=$(shell_out); echo "$OUT" | grep >/dev/null "ceo@e2e.local" && break; sleep 1; done
echo "$OUT" | grep >/dev/null "but this agent commits as Coder One" || fail "git did not show the identity refusal to the agent: $OUT"

step "Project filters, forge kind, and cancelling a task expires its pending approval"
[ "$(api GET "/projects/$PROJ" | json "d['data']['forge']")" = "gitea" ] || fail "project forge kind"
[ "$(api GET "/tasks?project_id=$PROJ" | json "all(t['project_id']=='$PROJ' for t in d['data']) and len(d['data'])>=1")" = "True" ] || fail "task project filter"
[ "$(api GET "/sessions?project_id=$PROJ&mode=chat" | json "len(d['data'])")" -ge 1 ] || fail "session project filter"
T2=$(api POST /tasks "{\"goal\":\"run: echo needs-approval\",\"project_id\":\"$PROJ\",\"autonomy\":2}" | json "d['data']['id']")
waiting() { api GET '/approvals?status=pending' | json "[a['id'] for a in d['data'] if a.get('task_id')=='$T2']" | grep >/dev/null apr_; }
wait_for "approval for the task" 60 waiting
AP2=$(api GET '/approvals?status=pending' | json "[a['id'] for a in d['data'] if a.get('task_id')=='$T2'][0]")
api POST "/tasks/$T2/cancel" >/dev/null
expired() { [ "$(api GET '/approvals' | json "[a['status'] for a in d['data'] if a['id']=='$AP2'][0]")" = "expired" ]; }
wait_for "approval expired after cancel" 15 expired

step "Default autonomy: L2 for project tasks, L1 otherwise, explicit L0 kept"
R=$(api POST /tasks "{\"goal\":\"hello\",\"project_id\":\"$PROJ\"}")
[ "$(echo "$R" | json "d['data']['autonomy']")" = "2" ] || fail "project task default autonomy: $R"
R=$(api POST /tasks "{\"goal\":\"hello\",\"agent_id\":\"$AGENT\"}")
[ "$(echo "$R" | json "d['data']['autonomy']")" = "1" ] || fail "plain task default autonomy: $R"
R=$(api POST /tasks "{\"goal\":\"hello\",\"project_id\":\"$PROJ\",\"autonomy\":0}")
[ "$(echo "$R" | json "d['data']['autonomy']")" = "0" ] || fail "explicit L0 overridden: $R"

step "Issue webhook: a labelled issue creates a task (signature checked, duplicates ignored)"
PAYLOAD=$(printf '{"action":"label_updated","issue":{"number":7,"title":"Add a /version endpoint","body":"Please add it.","html_url":"%s/akili/demo-svc/issues/7","state":"open","labels":[{"name":"akili"}]},"repository":{"name":"demo-svc","owner":{"login":"akili"}}}' "$GITEA")
SIG=$(printf '%s' "$PAYLOAD" | openssl dgst -sha256 -hmac hook-secret | awk '{print $NF}')
hook() { curl -sS -o /dev/null -w '%{http_code}' -H "X-Gitea-Event: issues" -H "X-Gitea-Signature: $1" -H 'Content-Type: application/json' -d "$PAYLOAD" "$API/webhooks/forge/$INT"; }
[ "$(hook deadbeef)" = "401" ] || fail "bad signature accepted"
[ "$(hook "$SIG")" = "201" ] || fail "labelled issue did not create a task"
[ "$(hook "$SIG")" = "200" ] || fail "duplicate issue event created a second task"
api GET /tasks | json "[t for t in d['data'] if t['trigger']=='issue' and t['trigger_ref']=='issue:akili/demo-svc#7']" | grep >/dev/null "Add a /version endpoint" || fail "issue task missing"

step "Maintenance preset and project template"
api POST "/projects/$PROJ/maintenance" '{"preset":"dependency-updates","enabled":false}' | json "d['data']['template']['project_id']" | grep >/dev/null "$PROJ" || fail "maintenance schedule"
TPL=$(api POST /projects "{\"integration_id\":\"$INT\",\"owner\":\"$GUSER\",\"repo\":\"orders-api\",\"create_repo\":true,\"template\":\"okapi-service\"}")
echo "$TPL" | json "d['data']['project']['sandbox_image']" | grep >/dev/null golang || fail "template sandbox image not applied"
echo "$TPL" | json "d['data']['task']['goal']" | grep >/dev/null "jkaninda/okapi" || fail "template scaffold task missing"

step "Audit chain verifies"
[ "$(api GET /audit/verify | json "d['data']['valid']")" = "True" ] || fail "audit chain invalid"

printf '\n\033[32mE2E CODER PASSED\033[0m\n'
