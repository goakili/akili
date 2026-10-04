// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package coder

// Template scaffolds a new project: its first task's goal and conventions saved on the project.
type Template struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	SandboxImage string `json:"sandbox_image"`
	Goal         string `json:"goal"`
	Instructions string `json:"instructions"`
}

// Templates are the built-in project templates.
var Templates = []Template{
	{
		ID:           "okapi-service",
		Name:         "Go service (Okapi)",
		Description:  "A Go HTTP API on github.com/jkaninda/okapi with route definitions, OpenAPI docs, health checks, tests, a Dockerfile and CI.",
		SandboxImage: "golang:1.27",
		Goal: `Scaffold a production-ready Go HTTP service in this repository using github.com/jkaninda/okapi v1.0.0.

Requirements:
- go.mod with module path matching the repository; Go 1.27.
- cmd/server/main.go starting Okapi with OpenAPI docs (okapi.OpenAPI, Scalar UI), graceful shutdown, config from environment variables (PORT, LOG_LEVEL) with github.com/jkaninda/go-utils (goutils.Env) and logging with github.com/jkaninda/logger.
- internal/routes: routes declared as data ([]okapi.RouteDefinition) grouped under /api/v1, with Summary, Request and Response types for OpenAPI.
- internal/handlers: GET /healthz and GET /readyz, plus an example resource (items: list, get, create) backed by an in-memory store, with typed handlers (okapi.H) and validation tags.
- Tests for the handlers (net/http/httptest) that pass with go test ./... .
- Dockerfile (multi-stage, CGO disabled, non-root runtime), .dockerignore, Makefile (build, test, run, docker), README with how to run.
- A CI workflow that runs go vet and go test on pushes and pull requests (.gitea/workflows/ci.yml for Gitea, .github/workflows/ci.yml for GitHub; add both).

Run go vet and go test in the sandbox until they pass, then commit, push and open a pull request describing the layout.`,
		Instructions: `Go service built on github.com/jkaninda/okapi. Declare routes as []okapi.RouteDefinition in internal/routes, keep handlers thin in internal/handlers, return JSON envelopes consistently, configure from environment variables, log with github.com/jkaninda/logger key/value pairs, and cover every handler with tests. Comments explain why, not what.`,
	},
	{
		ID:           "empty",
		Name:         "Empty repository",
		Description:  "Just the repository with a README; describe the first task yourself.",
		SandboxImage: "",
	},
}

// TemplateByID finds a template.
func TemplateByID(id string) (Template, bool) {
	for _, t := range Templates {
		if t.ID == id {
			return t, true
		}
	}
	return Template{}, false
}

// Preset is a recurring maintenance task offered on a project.
type Preset struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Cron        string `json:"cron"`
	Goal        string `json:"goal"`
}

// Presets are the built-in maintenance schedules.
var Presets = []Preset{
	{
		ID: "dependency-updates", Name: "Dependency updates", Cron: "0 6 * * 1",
		Description: "Weekly: update dependencies within their compatible range and open a PR if tests still pass.",
		Goal: "Update this project's dependencies to their latest compatible (non-breaking) versions. Run the full test suite; " +
			"if anything breaks, keep only the updates that pass. Open a pull request listing each update with old → new version. " +
			"If nothing needs updating, finish without a pull request and say so.",
	},
	{
		ID: "security-audit", Name: "Vulnerability scan", Cron: "0 7 * * *",
		Description: "Daily: scan for known vulnerabilities (govulncheck, npm audit, ...) and fix the ones with a safe upgrade.",
		Goal: "Scan this project for known vulnerabilities with the ecosystem's tool (govulncheck for Go, npm audit for Node, pip-audit for Python). " +
			"Fix every finding that has a safe upgrade, run the tests, and open a pull request with the findings and fixes. " +
			"Report findings you could not fix and why. If there are none, finish without a pull request.",
	},
	{
		ID: "test-health", Name: "Test health", Cron: "0 8 * * 3",
		Description: "Weekly: run the tests several times, find flaky or slow tests, and fix or report them.",
		Goal: "Run the test suite three times. Identify flaky tests (inconsistent results) and the slowest tests. Fix flaky tests where the cause is clear " +
			"and open a pull request; otherwise report each one with evidence. If everything is stable, finish with a short report.",
	},
	{
		ID: "docs-drift", Name: "Docs drift", Cron: "0 9 1 * *",
		Description: "Monthly: check README and docs against the code and fix what is out of date.",
		Goal: "Compare the README and documentation with the current code (commands, configuration, endpoints, flags). Fix anything out of date " +
			"and open a pull request. If the docs are accurate, finish with a short report.",
	},
}
