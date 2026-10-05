// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: Apache-2.0

package proto

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"testing"
)

func input(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func template(t *testing.T, name string) Policy {
	t.Helper()
	for _, p := range PolicyTemplates() {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("no template %q", name)
	return Policy{}
}

const wd = "/srv/akili"

// TestEveryToolDeniedByEmptyPolicy is the default-deny guarantee: a new tool added to the catalog is
// refused until a policy explicitly allows it.
func TestEveryToolDeniedByEmptyPolicy(t *testing.T) {
	for _, spec := range Catalog() {
		d := Evaluate(Policy{Name: "empty"}, AutonomyL3, wd, Call{Tool: spec.Name, Input: json.RawMessage(`{}`)})
		if d.Effect != EffectDeny {
			t.Errorf("%s: empty policy gave %s, want deny", spec.Name, d.Effect)
		}
	}
}

// TestEveryToolHasADenyCase makes each tool prove that a realistic policy refuses a hostile call.
func TestEveryToolHasADenyCase(t *testing.T) {
	cases := map[string]struct {
		policy string
		input  any
	}{
		ToolFSRead:            {"developer", FSReadInput{Path: "/etc/shadow"}},
		ToolFSList:            {"developer", FSListInput{Path: "/root/.ssh"}},
		ToolFSWrite:           {"developer", FSWriteInput{Path: "../../etc/cron.d/x", Content: "x"}},
		ToolFSEdit:            {"read-only", FSEditInput{Path: "a.txt", OldString: "a", NewString: "b"}},
		ToolSearch:            {"developer", SearchInput{Pattern: "x", Path: "/proc"}},
		ToolShell:             {"operator-safe", ShellInput{Command: "ls; rm -rf /"}},
		ToolHTTPFetch:         {"read-only", HTTPFetchInput{URL: "https://example.com"}},
		ToolHostInfo:          {"empty", HostInfoInput{}},
		ToolGitStatus:         {"operator-safe", EmptyInput{}},
		ToolGitDiff:           {"operator-safe", GitDiffInput{}},
		ToolGitCommit:         {"read-only", GitCommitInput{Message: "x"}},
		ToolGitPush:           {"read-only", EmptyInput{}},
		ToolSandboxExec:       {"developer", SandboxExecInput{Command: "sudo rm -rf /"}},
		ToolPROpen:            {"read-only", PROpenInput{Title: "x"}},
		ToolPRStatus:          {"operator-safe", EmptyInput{}},
		ToolServiceStatus:     {"developer", ServiceInput{Unit: "nginx"}},
		ToolServiceRestart:    {"operator", ServiceInput{Unit: "sshd"}},
		ToolJournalLogs:       {"read-only", JournalInput{}},
		ToolDiskUsage:         {"operator-safe", DiskUsageInput{Path: "/root/.ssh"}},
		ToolProcessList:       {"read-only", ProcessListInput{}},
		ToolDockerPS:          {"read-only", DockerPSInput{}},
		ToolDockerLogs:        {"operator-safe", ContainerInput{Container: "-evil --privileged"}},
		ToolDockerRestart:     {"operator", ContainerInput{Container: "akili-server"}},
		ToolCertCheck:         {"read-only", HostPortInput{Host: "example.com"}},
		ToolPackageUpdates:    {"read-only", EmptyInput{}},
		ToolNetProbe:          {"operator-safe", HostPortInput{Host: "evil.com/../x", Port: 22}},
		ToolChangeRun:         {"operator-safe", EmptyInput{}},
		ToolMiabiWorkspaces:   {"read-only", MiabiWorkspacesInput{}},
		ToolMiabiOverview:     {"read-only", MiabiWorkspaceInput{Workspace: "prod"}},
		ToolMiabiAlerts:       {"developer", MiabiWorkspaceInput{Workspace: "Prod"}},
		ToolMiabiAlert:        {"developer", MiabiAlertInput{Workspace: "prod", Alert: 1, Action: "delete"}},
		ToolMiabiEvents:       {"read-only", MiabiWorkspaceInput{Workspace: "prod"}},
		ToolMiabiDatabases:    {"developer", MiabiWorkspaceInput{}},
		ToolMiabiDBBackups:    {"developer", MiabiDatabaseInput{Workspace: "prod", Database: "../pg"}},
		ToolMiabiDBBackup:     {"operator-safe", MiabiDatabaseInput{Workspace: "prod", Database: "pg"}},
		ToolMiabiDBRestore:    {"developer", MiabiDatabaseInput{Workspace: "prod", Database: "pg"}},
		ToolMiabiTraffic:      {"read-only", MiabiTrafficInput{Workspace: "prod", App: "api"}},
		ToolMiabiEnv:          {"read-only", MiabiInput{Workspace: "prod", App: "api"}},
		ToolMiabiEnvSet:       {"developer", MiabiEnvSetInput{Workspace: "prod", App: "api", Key: "PATH; rm", Value: "x"}},
		ToolMiabiScale:        {"developer", MiabiScaleInput{Workspace: "prod", App: "api", Replicas: 1000}},
		ToolMiabiMaintenance:  {"operator-safe", MiabiMaintenanceInput{Workspace: "prod", App: "api", Enabled: true}},
		ToolMiabiCanary:       {"developer", MiabiCanaryInput{Workspace: "prod", App: "api", Action: "delete"}},
		ToolMiabiStackRestart: {"operator-safe", MiabiNamedInput{Workspace: "prod", Name: "web"}},
		ToolMiabiCronJobs:     {"read-only", MiabiWorkspaceInput{Workspace: "prod"}},
		ToolMiabiCronRun:      {"developer", MiabiNamedInput{Workspace: "prod", Name: "a:b"}},
		ToolMiabiPipelines:    {"read-only", MiabiWorkspaceInput{Workspace: "prod"}},
		ToolMiabiPipelineRun:  {"operator-safe", MiabiNamedInput{Workspace: "prod", Name: "build"}},
		ToolMiabiApps:         {"developer", MiabiAppsInput{Workspace: "Prod"}},
		ToolMiabiStatus:       {"developer", MiabiInput{App: "api"}}, // no workspace
		ToolMiabiDeployments:  {"read-only", MiabiInput{Workspace: "prod", App: "api"}},
		ToolMiabiReleases:     {"operator-safe", MiabiInput{Workspace: "prod", App: "../x"}},
		ToolMiabiLogs:         {"read-only", MiabiInput{Workspace: "prod", App: "api"}},
		ToolMiabiDeployLogs:   {"developer", MiabiDeployLogsInput{Workspace: "prod/x", App: "api", Deployment: 1}},
		ToolMiabiDeploy:       {"operator-safe", MiabiDeployInput{Workspace: "prod", App: "api"}},
		ToolMiabiRollback:     {"operator-safe", MiabiRollbackInput{Workspace: "prod", App: "api"}},
		ToolMiabiRestart:      {"read-only", MiabiInput{Workspace: "prod", App: "api"}},
		ToolLessonPropose:     {"developer", LessonInput{Lesson: "too short"}},
		ToolPlanPhaseUpdate:   {"read-only", PlanPhaseInput{Plan: "pln_1", Phase: "phs_1", Status: "done"}},
		ToolAskUser:           {"read-only", AskUserInput{Question: "Which database?", Options: []AskUserOption{{Label: "Postgres"}}}},
		ToolPlanPropose:       {"read-only", PlanProposeInput{Title: "Rewrite the API", Phases: make([]PlanProposalPhase, MaxProposedPhases+1)}},
	}
	for _, spec := range Catalog() {
		c, ok := cases[spec.Name]
		if !ok {
			t.Errorf("tool %s has no deny test case; add one", spec.Name)
			continue
		}
		p := Policy{Name: "empty"}
		if c.policy != "empty" {
			p = template(t, c.policy)
		}
		d := Evaluate(p, AutonomyL3, wd, Call{Tool: spec.Name, Input: input(t, c.input)})
		if d.Effect != EffectDeny {
			t.Errorf("%s under %s: got %s (%s), want deny", spec.Name, c.policy, d.Effect, d.Reason)
		}
	}
}

func TestEvaluate(t *testing.T) {
	tests := []struct {
		name     string
		policy   string
		autonomy Autonomy
		tool     string
		input    any
		want     string
	}{
		{"read in workdir auto at L1", "read-only", AutonomyL1, ToolFSRead, FSReadInput{Path: "main.go"}, EffectAllow},
		{"read-only at L0 needs approval", "read-only", AutonomyL0, ToolFSRead, FSReadInput{Path: "main.go"}, EffectApprove},
		{"read-only refuses write", "read-only", AutonomyL3, ToolFSWrite, FSWriteInput{Path: "a", Content: "b"}, EffectDeny},
		{"path traversal resolved then denied", "read-only", AutonomyL3, ToolFSRead, FSReadInput{Path: "../../../etc/shadow"}, EffectDeny},
		{"var log allowed", "read-only", AutonomyL1, ToolFSRead, FSReadInput{Path: "/var/log/syslog"}, EffectAllow},
		{"operator df needs approval at L2 (shell is high)", "operator-safe", AutonomyL2, ToolShell, ShellInput{Command: "df -h"}, EffectApprove},
		{"operator df auto at L3", "operator-safe", AutonomyL3, ToolShell, ShellInput{Command: "df -h"}, EffectAllow},
		{"operator refuses unknown command", "operator-safe", AutonomyL3, ToolShell, ShellInput{Command: "apt-get install x"}, EffectDeny},
		{"operator refuses chained command", "operator-safe", AutonomyL3, ToolShell, ShellInput{Command: "df -h && rm -rf /"}, EffectDeny},
		{"operator refuses subshell", "operator-safe", AutonomyL3, ToolShell, ShellInput{Command: "ls $(rm -rf /)"}, EffectDeny},
		{"developer denies sudo", "developer", AutonomyL3, ToolShell, ShellInput{Command: "sudo rm x"}, EffectDeny},
		{"developer denies force push", "developer", AutonomyL3, ToolShell, ShellInput{Command: "git push --force origin main"}, EffectDeny},
		{"developer denies reboot even with meta allowed", "developer", AutonomyL3, ToolShell, ShellInput{Command: "echo hi; reboot"}, EffectDeny},
		{"developer allows tests", "developer", AutonomyL3, ToolShell, ShellInput{Command: "go test ./..."}, EffectAllow},
		{"developer write auto at L2", "developer", AutonomyL2, ToolFSWrite, FSWriteInput{Path: "x.go", Content: "package x"}, EffectAllow},
		{"developer write outside workdir denied", "developer", AutonomyL3, ToolFSWrite, FSWriteInput{Path: "/etc/passwd", Content: "x"}, EffectDeny},
		{"full requires approval for shell even at L3", "full-with-approval", AutonomyL3, ToolShell, ShellInput{Command: "ls"}, EffectApprove},
		{"unknown tool denied", "developer", AutonomyL3, "rm_everything", struct{}{}, EffectDeny},
		{"malformed input denied", "developer", AutonomyL3, ToolFSRead, map[string]any{"path": "a", "evil": true}, EffectDeny},
		{"http private scheme denied", "developer", AutonomyL3, ToolHTTPFetch, HTTPFetchInput{URL: "file:///etc/passwd"}, EffectDeny},
		{"empty command denied", "developer", AutonomyL3, ToolShell, ShellInput{Command: "  "}, EffectDeny},
		{"full-no-approval shell auto at L3", "full-no-approval", AutonomyL3, ToolShell, ShellInput{Command: "apt-get update && apt-get install -y jq"}, EffectAllow},
		{"full-no-approval write anywhere auto at L3", "full-no-approval", AutonomyL3, ToolFSWrite, FSWriteInput{Path: "/opt/app/config.yml", Content: "x"}, EffectAllow},
		{"full-no-approval still bounded by autonomy", "full-no-approval", AutonomyL1, ToolShell, ShellInput{Command: "ls"}, EffectApprove},
		{"full-no-approval keeps the deny floor", "full-no-approval", AutonomyL3, ToolShell, ShellInput{Command: "sudo reboot"}, EffectDeny},
		{"full-no-approval protects credentials", "full-no-approval", AutonomyL3, ToolFSRead, FSReadInput{Path: "/root/.ssh/id_ed25519"}, EffectDeny},
		{"developer commits at L2", "developer", AutonomyL2, ToolGitCommit, GitCommitInput{Message: "feat: add endpoint"}, EffectAllow},
		{"developer pushes at L2", "developer", AutonomyL2, ToolGitPush, EmptyInput{}, EffectAllow},
		{"developer opens PR at L2", "developer", AutonomyL2, ToolPROpen, PROpenInput{Title: "Add endpoint"}, EffectAllow},
		{"sandbox tests auto at L2", "developer", AutonomyL2, ToolSandboxExec, SandboxExecInput{Command: "go test ./... 2>&1 | tail -50"}, EffectAllow},
		{"read-only can inspect git", "read-only", AutonomyL1, ToolGitDiff, GitDiffInput{}, EffectAllow},
		{"commit needs a message", "developer", AutonomyL3, ToolGitCommit, GitCommitInput{}, EffectDeny},
		{"operator status any unit at L1", "operator", AutonomyL1, ToolServiceStatus, ServiceInput{Unit: "nginx"}, EffectAllow},
		{"operator restart needs approval at L2", "operator", AutonomyL2, ToolServiceRestart, ServiceInput{Unit: "nginx"}, EffectApprove},
		{"operator never restarts the agent", "operator", AutonomyL3, ToolServiceRestart, ServiceInput{Unit: "akili-agent"}, EffectDeny},
		{"operator shell always approved", "operator", AutonomyL3, ToolShell, ShellInput{Command: "ls"}, EffectApprove},
		{"operator-safe reads journald", "operator-safe", AutonomyL1, ToolJournalLogs, JournalInput{Unit: "nginx", Since: "1 hour ago"}, EffectAllow},
		{"unit with flags refused", "operator", AutonomyL3, ToolServiceStatus, ServiceInput{Unit: "--all nginx"}, EffectDeny},
		{"operator reads miabi status at L1", "operator", AutonomyL1, ToolMiabiStatus, MiabiInput{Workspace: "prod", App: "api"}, EffectAllow},
		{"operator deploy needs approval at L2", "operator", AutonomyL2, ToolMiabiDeploy, MiabiDeployInput{Workspace: "prod", App: "api", Tag: "v2"}, EffectApprove},
		{"operator kubectl get pods auto at L3", "operator-safe", AutonomyL3, ToolShell, ShellInput{Command: "kubectl get pods -A"}, EffectAllow},
		{"operator kubectl secrets denied", "operator-safe", AutonomyL3, ToolShell, ShellInput{Command: "kubectl get secrets -n default"}, EffectDeny},
		{"operator kubectl secrets after flags denied", "operator-safe", AutonomyL3, ToolShell, ShellInput{Command: "kubectl get -n kube-system secret"}, EffectDeny},
		{"operator kubectl secret by name denied", "operator-safe", AutonomyL3, ToolShell, ShellInput{Command: "kubectl describe secret/db-creds"}, EffectDeny},
		{"operator kubectl delete denied", "operator-safe", AutonomyL3, ToolShell, ShellInput{Command: "kubectl delete pod x"}, EffectDeny},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := Evaluate(template(t, tt.policy), tt.autonomy, wd, Call{Tool: tt.tool, Input: input(t, tt.input)})
			if d.Effect != tt.want {
				t.Fatalf("got %s (%s), want %s", d.Effect, d.Reason, tt.want)
			}
		})
	}
}

func TestPathMatch(t *testing.T) {
	tests := []struct {
		pattern, path string
		want          bool
	}{
		{"/srv/**", "/srv", true},
		{"/srv/**", "/srv/a/b/c", true},
		{"/srv/*", "/srv/a/b", false},
		{"/home/*/.ssh/**", "/home/bob/.ssh/id_rsa", true},
		{"**/.akili-agent/**", "/var/lib/.akili-agent/state.json", true},
		{"/var/log/**", "/var/logs/x", false},
	}
	for _, tt := range tests {
		if got := pathMatch(tt.pattern, tt.path); got != tt.want {
			t.Errorf("pathMatch(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
		}
	}
}

func TestDomainMatch(t *testing.T) {
	if !domainMatch("*.github.com", "api.github.com") || domainMatch("*.github.com", "github.com") || domainMatch("*.github.com", "evilgithub.com") {
		t.Fatal("domain matching is wrong")
	}
}

func TestSignedPolicy(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	sp, err := SignPolicy(template(t, "developer"), priv)
	if err != nil {
		t.Fatal(err)
	}
	p, err := sp.Verify(pub)
	if err != nil || p.Name != "developer" {
		t.Fatalf("verify: %v %q", err, p.Name)
	}
	sp.Policy = []byte(`{"name":"developer","tools":{"allow":["*"]},"max_risk":"critical"}`)
	if _, err := sp.Verify(pub); err == nil {
		t.Fatal("tampered policy verified")
	}
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	sp2, _ := SignPolicy(template(t, "developer"), priv)
	if _, err := sp2.Verify(other); err == nil {
		t.Fatal("policy verified with the wrong key")
	}
}

func TestPathAllowed(t *testing.T) {
	p := template(t, "developer")
	if !p.PathAllowed(wd, wd+"/src/main.go") || p.PathAllowed(wd, "/etc/shadow") || p.PathAllowed(wd, "/home/bob/.ssh/id_rsa") {
		t.Fatal("PathAllowed is wrong")
	}
}

func TestRiskRoundTrip(t *testing.T) {
	for _, r := range []Risk{0, RiskLow, RiskCritical} {
		b, _ := json.Marshal(r)
		var back Risk
		if err := json.Unmarshal(b, &back); err != nil || back != r {
			t.Errorf("risk %d: %s → %d (%v)", r, b, back, err)
		}
	}
}

func TestParseChangePlan(t *testing.T) {
	good := `{"title":"Clean /var/tmp","reason":"disk 97% full","steps":[{"tool":"shell","input":{"command":"rm -rf /var/tmp/cache/*"}}],"verify":[{"tool":"disk_usage","input":{"path":"/"},"expect":"/"}],"rollback":[]}`
	if _, err := ParseChangePlan(json.RawMessage(good)); err != nil {
		t.Fatalf("valid plan rejected: %v", err)
	}
	bad := map[string]string{
		"no verify":          `{"title":"x","reason":"y","steps":[{"tool":"host_info","input":{}}],"verify":[]}`,
		"no steps":           `{"title":"x","reason":"y","steps":[],"verify":[{"tool":"host_info","input":{}}]}`,
		"nested change":      `{"title":"x","reason":"y","steps":[{"tool":"change_run","input":{}}],"verify":[{"tool":"host_info","input":{}}]}`,
		"mutating verify":    `{"title":"x","reason":"y","steps":[{"tool":"host_info","input":{}}],"verify":[{"tool":"service_restart","input":{"unit":"nginx"}}]}`,
		"unknown tool":       `{"title":"x","reason":"y","steps":[{"tool":"nuke","input":{}}],"verify":[{"tool":"host_info","input":{}}]}`,
		"bad step input":     `{"title":"x","reason":"y","steps":[{"tool":"service_status","input":{"unit":"-x"}}],"verify":[{"tool":"host_info","input":{}}]}`,
		"no reason":          `{"title":"x","reason":"","steps":[{"tool":"host_info","input":{}}],"verify":[{"tool":"host_info","input":{}}]}`,
		"unknown plan field": `{"title":"x","reason":"y","approved":true,"steps":[{"tool":"host_info","input":{}}],"verify":[{"tool":"host_info","input":{}}]}`,
	}
	for name, plan := range bad {
		if _, err := ParseChangePlan(json.RawMessage(plan)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestCallHashIgnoresFormatting(t *testing.T) {
	if CallHash("shell", json.RawMessage(`{"command": "ls"}`)) != CallHash("shell", json.RawMessage(`{"command":"ls"}`)) {
		t.Fatal("whitespace changed the hash")
	}
	if CallHash("shell", json.RawMessage(`{"command":"ls"}`)) == CallHash("shell", json.RawMessage(`{"command":"ls -la"}`)) {
		t.Fatal("different inputs share a hash")
	}
}

func TestAllowsToolOffersEveryAllowedTool(t *testing.T) {
	op := template(t, "operator")
	for _, name := range []string{ToolChangeRun, ToolServiceStatus, ToolServiceRestart, ToolDiskUsage, ToolShell} {
		if !op.AllowsTool(name) {
			t.Errorf("operator policy should offer %s", name)
		}
	}
	if op.AllowsTool(ToolGitPush) || template(t, "read-only").AllowsTool(ToolShell) || (Policy{}).AllowsTool(ToolFSRead) {
		t.Error("offered a tool the policy does not allow")
	}
}

func TestMiabiPlanIsValid(t *testing.T) {
	plan := `{"title":"Deploy api v3","reason":"release","steps":[{"tool":"miabi_deploy","input":{"workspace":"prod","app":"api","tag":"v3"}}],
		"verify":[{"tool":"miabi_status","input":{"workspace":"prod","app":"api"},"expect":"health: healthy"}],"rollback":[{"tool":"miabi_rollback","input":{"workspace":"prod","app":"api"}}]}`
	if _, err := ParseChangePlan(json.RawMessage(plan)); err != nil {
		t.Fatalf("deploy/verify/rollback plan rejected: %v", err)
	}
	p := template(t, "operator")
	p.Apps = Rule{Allow: []string{"web-*"}}
	if d := Evaluate(p, AutonomyL3, wd, Call{Tool: ToolMiabiDeploy, Input: input(t, MiabiDeployInput{Workspace: "prod", App: "api"})}); d.Effect != EffectDeny {
		t.Fatalf("app outside the allow list: %s", d.Effect)
	}
}

// Every template, at every autonomy, must refuse the agent's own key and its own containers.
func TestTemplatesProtectTheAgent(t *testing.T) {
	calls := []Call{
		{Tool: ToolFSRead, Input: json.RawMessage(`{"path":"/var/lib/akili-agent/state.json"}`)},
		{Tool: ToolFSWrite, Input: json.RawMessage(`{"path":"/var/lib/akili-agent/state.json","content":"{}"}`)},
		{Tool: ToolFSWrite, Input: json.RawMessage(`{"path":"/var/lib/akili-agent/.ssh/authorized_keys","content":"k"}`)},
		{Tool: ToolDockerRestart, Input: json.RawMessage(`{"container":"akili-agent"}`)},
		{Tool: ToolServiceRestart, Input: json.RawMessage(`{"unit":"akili-agent.service"}`)},
	}
	for _, p := range PolicyTemplates() {
		for _, a := range []Autonomy{AutonomyL0, AutonomyL1, AutonomyL2, AutonomyL3} {
			for _, c := range calls {
				if d := Evaluate(p, a, wd, c); d.Effect != EffectDeny {
					t.Errorf("%s at L%d: %s %s = %s", p.Name, a, c.Tool, c.Input, d.Effect)
				}
			}
		}
	}
}

// Workspace-qualified app rules: an agent may work freely in staging, never touch production apps,
// and cannot reach production by leaving the workspace out.
func TestMiabiWorkspaceRules(t *testing.T) {
	p := template(t, "operator")
	p.Apps = Rule{Allow: []string{"staging/*", "prod/status-page"}, Deny: []string{"prod/*"}}
	cases := []struct {
		ws, app string
		want    string
	}{
		{"staging", "api", EffectApprove}, // allowed app; deploy is high risk at L2
		{"prod", "api", EffectDeny},
		{"prod", "status-page", EffectDeny}, // deny wins over allow
		{"", "api", EffectDeny},
		{"Prod", "api", EffectDeny}, // not a handle
		{"prod/../staging", "api", EffectDeny},
		{"dev", "api", EffectDeny}, // not allowed at all
	}
	for _, c := range cases {
		d := Evaluate(p, AutonomyL2, wd, Call{Tool: ToolMiabiDeploy, Input: input(t, MiabiDeployInput{Workspace: c.ws, App: c.app})})
		if d.Effect != c.want {
			t.Errorf("deploy %s/%s = %s (%s), want %s", c.ws, c.app, d.Effect, d.Reason, c.want)
		}
	}
	if d := Evaluate(p, AutonomyL2, wd, Call{Tool: ToolMiabiApps, Input: input(t, MiabiAppsInput{Workspace: "prod"})}); d.Effect != EffectDeny {
		t.Errorf("listing a denied workspace: %s", d.Effect)
	}
	if d := Evaluate(p, AutonomyL2, wd, Call{Tool: ToolMiabiApps, Input: input(t, MiabiAppsInput{Workspace: "staging"})}); d.Effect != EffectAllow {
		t.Errorf("listing staging: %s (%s)", d.Effect, d.Reason)
	}
}

// "prod/*" covers every kind of Miabi resource, and a restore always needs a human.
func TestMiabiResourceKinds(t *testing.T) {
	p := template(t, "operator")
	p.Apps = Rule{Allow: []string{"*"}, Deny: []string{"prod/*"}}
	for _, c := range []Call{
		{Tool: ToolMiabiDBBackup, Input: input(t, MiabiDatabaseInput{Workspace: "prod", Database: "pg-main"})},
		{Tool: ToolMiabiStackRestart, Input: input(t, MiabiNamedInput{Workspace: "prod", Name: "web"})},
		{Tool: ToolMiabiCronRun, Input: input(t, MiabiNamedInput{Workspace: "prod", Name: "nightly"})},
		{Tool: ToolMiabiPipelineRun, Input: input(t, MiabiNamedInput{Workspace: "prod", Name: "build"})},
		{Tool: ToolMiabiOverview, Input: input(t, MiabiWorkspaceInput{Workspace: "prod"})},
		{Tool: ToolMiabiScale, Input: input(t, MiabiScaleInput{Workspace: "prod", App: "api", Replicas: 2})},
	} {
		if d := Evaluate(p, AutonomyL3, wd, c); d.Effect != EffectDeny {
			t.Errorf("%s in prod: %s", c.Tool, d.Effect)
		}
	}
	restore := Call{Tool: ToolMiabiDBRestore, Input: input(t, MiabiDatabaseInput{Workspace: "staging", Database: "pg-main", Backup: 3})}
	// The operator template stops at high risk: restores need a policy that allows critical actions.
	if d := Evaluate(p, AutonomyL3, wd, restore); d.Effect != EffectDeny {
		t.Errorf("restore under the operator template: %s", d.Effect)
	}
	p.MaxRisk = RiskCritical
	if d := Evaluate(p, AutonomyL3, wd, restore); d.Effect != EffectApprove || d.Risk != RiskCritical {
		t.Errorf("restore at L3: %s %s", d.Effect, d.Risk)
	}
	if d := Evaluate(p, AutonomyL3, wd, Call{Tool: ToolMiabiDBBackup, Input: input(t, MiabiDatabaseInput{Workspace: "staging", Database: "pg-main"})}); d.Effect != EffectAllow {
		t.Errorf("staging backup at L3: %s (%s)", d.Effect, d.Reason)
	}
}

// MCP tools are evaluated by name and their assigned risk; unregistered or malformed names are denied.
func TestDynamicMCPTools(t *testing.T) {
	SetDynamicTools("mcp__miabi__", []DynamicTool{
		{Name: "mcp__miabi__list_apps", Risk: RiskLow, InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "mcp__miabi__deploy_app", Risk: RiskHigh, InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "mcp__other__sneaky", Risk: RiskLow},     // wrong prefix: ignored
		{Name: "mcp__miabi__bad name", Risk: RiskLow},   // malformed: ignored
		{Name: "mcp__miabi__no_risk", InputSchema: nil}, // no risk: ignored
	})
	defer SetDynamicTools("mcp__miabi__", nil)
	p := Policy{Name: "p", Tools: Rule{Allow: []string{"mcp__miabi__*"}}, MaxRisk: RiskHigh}
	args := json.RawMessage(`{"workspace":"prod"}`)
	cases := []struct {
		tool string
		a    Autonomy
		in   json.RawMessage
		want string
	}{
		{"mcp__miabi__list_apps", AutonomyL1, args, EffectAllow},
		{"mcp__miabi__deploy_app", AutonomyL2, args, EffectApprove},
		{"mcp__miabi__deploy_app", AutonomyL3, args, EffectAllow},
		{"mcp__miabi__list_apps", AutonomyL1, json.RawMessage(`["x"]`), EffectDeny}, // not an object
		{"mcp__other__sneaky", AutonomyL3, args, EffectDeny},
		{"mcp__miabi__no_risk", AutonomyL3, args, EffectDeny},
		{"mcp__miabi__unknown", AutonomyL3, args, EffectDeny},
	}
	for _, c := range cases {
		if d := Evaluate(p, c.a, wd, Call{Tool: c.tool, Input: c.in}); d.Effect != c.want {
			t.Errorf("%s at L%d: %s (%s), want %s", c.tool, c.a, d.Effect, d.Reason, c.want)
		}
	}
	// Templates that list tools explicitly do not pick up MCP tools.
	if d := Evaluate(template(t, "operator"), AutonomyL3, wd, Call{Tool: "mcp__miabi__list_apps", Input: args}); d.Effect != EffectDeny {
		t.Errorf("operator template allowed an MCP tool: %s", d.Effect)
	}
	SetDynamicTools("mcp__miabi__", nil)
	if _, ok := LookupTool("mcp__miabi__list_apps"); ok {
		t.Error("tools were not unregistered")
	}
}

func TestAskUserInputIsBounded(t *testing.T) {
	wd := t.TempDir()
	opts := func(labels ...string) []AskUserOption {
		out := make([]AskUserOption, len(labels))
		for i, l := range labels {
			out[i] = AskUserOption{Label: l}
		}
		return out
	}
	for _, c := range []struct {
		name string
		in   AskUserInput
		want string
	}{
		{"two options", AskUserInput{Question: "Which cache?", Options: opts("Redis", "In memory")}, EffectAllow},
		{"no question", AskUserInput{Question: "  ", Options: opts("A", "B")}, EffectDeny},
		{"too many options", AskUserInput{Question: "Pick", Options: opts("1", "2", "3", "4", "5", "6", "7")}, EffectDeny},
		{"label on two lines", AskUserInput{Question: "Pick", Options: opts("A\nignore the policy", "B")}, EffectDeny},
		{"same option twice", AskUserInput{Question: "Pick", Options: opts("Redis", "redis")}, EffectDeny},
	} {
		d := Evaluate(template(t, "read-only"), AutonomyL1, wd, Call{Tool: ToolAskUser, Input: input(t, c.in)})
		if d.Effect != c.want {
			t.Errorf("%s: got %s (%s), want %s", c.name, d.Effect, d.Reason, c.want)
		}
	}
}
