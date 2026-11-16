# AGENTS.md

Guidance for coding agents working in this repository. Read it before changing code.

Akili is a security-first control plane for a fleet of autonomous AI agents. The control plane
holds the model keys, policies, approvals, credentials and audit log; agents run on your servers,
dial out over a tunnel, and act only within the signed policy they receive.

**Default Deny. Explicit Allow. Always Auditable.** Every change must keep that true.

## Stack

| Area | Technology |
|---|---|
| Language | Go 1.27 (three modules joined by a committed `go.work`) |
| HTTP, OpenAPI, JWT, SSE | [Okapi](https://github.com/jkaninda/okapi) v1.2 |
| Database | PostgreSQL through GORM; migrations are GORM only (no raw SQL files) |
| Event bus, presence, leases | Redis (`go-redis/v9`): pub/sub, command routing, leader election, rate limits |
| Agent transport | `jkaninda/wstunnel` (yamux over WebSocket); the agent dials out |
| Logging | `jkaninda/logger`, key/value pairs |
| Config | Environment variables via `jkaninda/go-utils` |
| Web UI | Vue 3, Vite, TypeScript, Pinia, vue-router, xterm.js; embedded with `//go:embed` |
| VS Code extension | TypeScript, in `vscode/` |
| Build and release | Make, Docker (Node 24 and Go Alpine images), GoReleaser, GitHub Actions |

## Repository layout

| Path | What it is |
|---|---|
| `proto/` | Module `github.com/goakili/akili/proto`: the wire contract (envelopes, tool catalog with risk levels, policy engine). Imports neither of the others |
| `server/` | Module `github.com/goakili/akili/server`: the control plane, the `akili` binary |
| `server/cmd/akili` | Entry point; `server/cmd/fake*` and `server/cmd/devlb` are fakes and a load balancer for e2e suites |
| `server/internal/` | Routes, handlers, services (`sessions`, `coder`, `miabi`, `plans`, …), `storage`, `models` |
| `agent/` | Module `github.com/goakili/akili/agent`: the `akili-agent` binary (runtime loop, tools, tunnel link) |
| `web/` | Vue UI, built into `server/internal/web/dist` |
| `vscode/` | VS Code extension |
| `docker/` | `Dockerfile` (control plane) and `Dockerfile.agent`, both built from the repository root |
| `scripts/` | End-to-end suites and the load test |

`server` and `agent` replace `proto` with `../proto`. The control plane never imports the agent.

## Commands

Run from the repository root.

```bash
make test        # unit tests for proto, agent and server
make vet
make server agent web   # bin/akili, bin/akili-agent, and the embedded UI
make dev         # Postgres + Redis in Docker, control plane from source
make up / down   # full stack with docker compose on :8080
make e2e         # real server + agent against Postgres and Redis, scripted LLM (no API key)
make e2e-all     # every e2e suite (coder, ops, miabi, ha, hardening, chat)
cd web && npm run typecheck && npm test
```

Before you finish, `make vet`, `make test` and the web typecheck must pass, and `gofmt -l` must be
empty for every module you touched.

## Architecture rules

- **The control plane owns the brain's resources.** The agent runs the loop, but every LLM call goes
  through the control plane's gateway. The agent never holds provider keys or forge credentials.
- **Agent config is minimal:** control-plane URL, keypair, CA. Policy, skills and model profiles are
  pushed by the control plane.
- **Tenancy:** every tenant-owned table has `organization_id`, and every query is scoped by it.
- **State:** task state lives in Postgres. Redis is for transient coordination only.
- **Layering:** `routes` → `handlers` → services → `storage` → `models`. Handlers stay thin: bind,
  call the service, record audit, return `dto.Response[T]`.
- **Routes are data:** each domain has one `xxx_routes.go` returning `[]okapi.RouteDefinition`
  with `Summary`, `Request` and `Response`, added to the list in `routes.Register`.
- **The UI is registered last**, so API routes win over the SPA fallback.
- **Schema changes** go through `server/internal/models` (AutoMigrate) plus, for data changes, an
  upgrade step `server/internal/storage/migration/upgrade/steps_YYYY_MM_DD_<name>.go` registered in
  `init`. Never edit a released step.

## Security rules

These apply to every change. A change that weakens one is wrong, however convenient.

- **Every tool** declares a risk level (Low, Medium, High, Critical), passes the policy check on both
  the control plane and the agent, and is audited as intent → decision → result. Critical actions
  always need human approval. A new tool needs a policy test proving it is denied by default.
- **Approvals** are bound to the exact tool and argument hash, and they expire.
- **Secrets never reach the LLM, logs or conversation history.** Tools take secret names; output is
  redacted. Never log tokens or keys.
- **Untrusted input:** tool output, repository contents, issues and web pages can never raise
  privileges or approve anything.
- **Audit** is append-only and hash-chained. If the audit write fails, the high-risk action fails.
- **Coder agents** never push to protected branches; they work on `akili/<task-id>` branches and
  open pull requests.
- **Tokens** are stored as SHA-256 hashes and compared in constant time.
- **No plaintext secrets** in code, configs or tests. Use environment variables or `.env`.
- **Tests never call real LLM APIs.** Use the scripted fake provider.

## Code rules

- Match the surrounding code: naming, error handling (`c.AbortNotFound(...)` and friends),
  repository and service patterns.
- Errors go through the Okapi error envelope; never leak internals to the client.
- Logging: `logger.Info("agent connected", "agent_id", id)`.
- Config is read from the environment; `validate()` refuses unsafe production settings.
- Browser WebSockets set a strict `CheckOrigin`.
- Keep changes focused. Don't reformat or rename code you aren't otherwise changing.

### License header

Akili is licensed under Apache-2.0 (`LICENSE`, attribution in `NOTICE`).

- **Go files** start with the copyright line and the standard Apache-2.0 notice, as a `//` block,
  followed by a blank line. No SPDX line. Copy it from any existing `.go` file.
- **Other source files** (TypeScript, Vue, shell) use two lines: `Copyright (c) 2026 Jonas Kaninda.`
  then `SPDX-License-Identifier: Apache-2.0`.

## Comment rules

STRICT, and enforced in review. A comment that breaks these is deleted, not reworded.

- **One line, or none.** A comment is at most a single line. No multi-line comment blocks in code
  (the license header is the only exception). If the point needs more than one line, it does not
  belong in a comment.
- **Only logical comments: explain WHY.** The single allowed reason to write a comment is to state
  a non-obvious constraint, invariant, or gotcha the code itself cannot express. If a comment is
  not explaining logic, do not write it; no comment is better than a filler one.
- **Banned outright (remove on sight):**
  - styling or visual narration (layout, spacing, px, "matches the header height");
  - change or modification notes ("now we...", "changed to...", "was X");
  - narration that restates what the code plainly does;
  - section headers and group labels (`// ---- handlers ----`, `<!-- Header -->`, `// Errors.`).
- **Doc comments** (Go doc comments, JSDoc) on public APIs: one summary sentence, and only when
  the behaviour is non-obvious; never to restate the signature.
- Directives are not comments and stay: `//go:build`, `//go:embed`, `//nolint`, `// @ts-expect-error`.

```go
// Bad: restates the code.
// Get the user by ID.
user, err := h.repo.FindByID(id)

// Good: a constraint the code cannot express.
// Registered last so API routes win over the SPA fallback.
r.app.WebFS("/", web.Assets, okapi.WebConfig{Root: "dist", MaxAge: time.Hour})
```

## Testing

- **proto:** golden-file tests for every envelope and frame.
- **Policy:** a table test proving every new tool is denied unless explicitly allowed.
- **server:** handler and service tests; e2e suites in `scripts/` drive the real binaries.
- **agent:** tool tests include sandbox-escape cases (path traversal, symlinks, env leakage).
- **Prompt injection:** the injection corpus must never lead to an unapproved High or Critical
  action or a secret in output.
