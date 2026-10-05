VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)

GO_TAGS            ?= enterprise
LICENSE_PUBLIC_KEY ?=
LDFLAGS_SERVER = -s -w -X github.com/goakili/akili/server/internal/config.Version=$(VERSION) \
	-X github.com/goakili/akili/server/internal/enterprise.embeddedPublicKey=$(LICENSE_PUBLIC_KEY)
DOCKER_ARGS    = --build-arg VERSION=$(VERSION) --build-arg GO_TAGS=$(GO_TAGS) --build-arg LICENSE_PUBLIC_KEY=$(LICENSE_PUBLIC_KEY)
LDFLAGS_AGENT  = -s -w -X main.Version=$(VERSION)

-include .env
export

REGISTRY    ?= jkaninda
IMAGE_TAG   ?= $(VERSION)
PLATFORMS   ?= linux/amd64,linux/arm64
BUILDER     ?= akili-builder
SERVER_IMAGE = $(REGISTRY)/akili
AGENT_IMAGE  = $(REGISTRY)/akili-agent

AKILI_URL       ?= $(or $(AKILI_PUBLIC_URL),http://localhost:9000)
AGENT_STATE_DIR ?= .agent/state
AGENT_WORKDIR   ?= .agent/work

.PHONY: all build-ui web vscode vscode-test vscode-package server agent run run-agent e2e-coder e2e-gitlab e2e-ops e2e-miabi e2e-ha e2e-hardening e2e-chat e2e-all loadtest docker-build docker-build-server docker-build-agent docker-builder docker-push docker-push-server docker-push-agent test vet dev up down e2e clean

all: build-ui server agent


build-ui: web/node_modules/.package-lock.json
	cd web && npm run build

web/node_modules/.package-lock.json: web/package-lock.json
	cd web && npm ci

web: build-ui

vscode: web/node_modules/.package-lock.json vscode/node_modules/.package-lock.json
	cd vscode && npm run build

vscode-test: vscode/node_modules/.package-lock.json
	cd vscode && npm test

vscode-package: vscode
	cd vscode && npx @vscode/vsce package

vscode/node_modules/.package-lock.json: vscode/package-lock.json
	cd vscode && npm ci

server:
	cd server && CGO_ENABLED=0 go build -trimpath -tags "$(GO_TAGS)" -ldflags "$(LDFLAGS_SERVER)" -o $(CURDIR)/bin/akili ./cmd/akili

agent:
	cd agent && CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS_AGENT)" -o $(CURDIR)/bin/akili-agent ./cmd/akili-agent

docker-build: docker-build-server docker-build-agent

docker-build-server:
	docker buildx build --load -f docker/Dockerfile $(DOCKER_ARGS) -t $(SERVER_IMAGE):$(IMAGE_TAG) -t $(SERVER_IMAGE):latest .

docker-build-agent:
	docker buildx build --load -f docker/Dockerfile.agent --build-arg VERSION=$(VERSION) \
		-t $(AGENT_IMAGE):$(IMAGE_TAG) -t $(AGENT_IMAGE):latest .

docker-builder:
	@docker buildx inspect $(BUILDER) >/dev/null 2>&1 || docker buildx create --name $(BUILDER) --driver docker-container >/dev/null

docker-push: docker-push-server docker-push-agent

docker-push-server: docker-builder
	docker buildx build --builder $(BUILDER) --platform $(PLATFORMS) -f docker/Dockerfile $(DOCKER_ARGS) \
		-t $(SERVER_IMAGE):$(IMAGE_TAG) -t $(SERVER_IMAGE):latest --push .

docker-push-agent: docker-builder
	docker buildx build --builder $(BUILDER) --platform $(PLATFORMS) -f docker/Dockerfile.agent \
		--build-arg VERSION=$(VERSION) -t $(AGENT_IMAGE):$(IMAGE_TAG) -t $(AGENT_IMAGE):latest --push .

MODULES = proto agent server

# Unit tests of every module; the server in both editions.
test:
	@for m in $(MODULES); do (cd $$m && go test ./...) || exit 1; done
	cd server && go test -tags enterprise ./...

vet:
	@for m in $(MODULES); do (cd $$m && go vet ./...) || exit 1; done
	cd server && go vet -tags enterprise ./...


run: server
	@test -f .env || cp .env.example .env
	./bin/akili server

# Run a local agent against the control plane (make run). The first run enrolls with AKILI_JOIN_TOKEN
# (from .env or the command line: make run-agent AKILI_JOIN_TOKEN=akj_...); later runs reuse the state.
# To enroll again with a new token: make run-agent AKILI_JOIN_TOKEN=akj_... ENROLL_FLAGS=--force
run-agent: agent
	@if [ ! -f $(AGENT_STATE_DIR)/state.json ] || [ -n "$(ENROLL_FLAGS)" ]; then \
		if [ -z "$(AKILI_JOIN_TOKEN)" ]; then \
			echo "AKILI_JOIN_TOKEN is required to enroll: create an agent in the UI (Agents → Add agent) and set it in .env"; exit 1; \
		fi; \
		AKILI_LOG_FORMAT=text ./bin/akili-agent enroll --url $(AKILI_URL) --state-dir $(AGENT_STATE_DIR) --workdir $(AGENT_WORKDIR) $(ENROLL_FLAGS); \
	fi
	AKILI_LOG_FORMAT=text ./bin/akili-agent start --state-dir $(AGENT_STATE_DIR)

# Run the control plane on the host against the compose Postgres/Redis. Settings come from .env
# (cp .env.example .env); run `cd web && npm run dev` alongside for the UI with hot reload.
dev:
	@test -f .env || cp .env.example .env
	docker compose up -d postgres redis
	go run ./server/cmd/akili server

up:
	docker compose up -d --build

down:
	docker compose down

e2e:
	./scripts/e2e.sh

# Coding flow against a real Gitea in Docker (integration, project, PR, push guard, webhooks).
e2e-coder:
	./scripts/e2e-coder.sh

# The coding flow against a real GitLab CE (about 4 GB of memory, a few minutes to boot). Opt-in:
# not part of e2e-all.
e2e-gitlab:
	./scripts/e2e-gitlab.sh

# Operations: alert → triage → approved change plan → verify / rollback, recorded terminal.
e2e-ops:
	./scripts/e2e-ops.sh

# Miabi against a fake Miabi API: signed webhook → verify → approved rollback; deploy with auto-rollback.
e2e-miabi:
	./scripts/e2e-miabi.sh

# Chaos: two replicas behind a load balancer; the tunnel's replica is killed mid-task and the task resumes.
e2e-ha:
	./scripts/e2e-ha.sh

# TLS + agent mTLS, SSO (fake OIDC), SIEM sinks, KMS migration to Vault Transit, forwarded-header spoofing.
e2e-hardening:
	./scripts/e2e-hardening.sh

# Chat gateways (fake Telegram, Slack, Signal) and lessons.
e2e-chat:
	./scripts/e2e-chat.sh

e2e-all: e2e e2e-coder e2e-ops e2e-miabi e2e-ha e2e-hardening e2e-chat

# Simulated fleet: AGENTS (1000) agents and TASKS (200) tasks against one control plane.
loadtest:
	./scripts/loadtest.sh

clean:
	rm -rf bin .agent
