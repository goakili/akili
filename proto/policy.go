// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: Apache-2.0

package proto

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
)

// Autonomy is how much an agent may do without a human.
//
//	L0 suggest only: every tool call needs approval
//	L1 auto-run Low
//	L2 auto-run Low and Medium
//	L3 auto-run up to High
//
// Critical always needs approval, at every level.
type Autonomy int

const (
	AutonomyL0 Autonomy = iota
	AutonomyL1
	AutonomyL2
	AutonomyL3
)

// autoMax is the highest risk each level runs without approval.
func (a Autonomy) autoMax() Risk {
	switch a {
	case AutonomyL1:
		return RiskLow
	case AutonomyL2:
		return RiskMedium
	case AutonomyL3:
		return RiskHigh
	default:
		return 0
	}
}

// Valid reports whether a is a defined level.
func (a Autonomy) Valid() bool { return a >= AutonomyL0 && a <= AutonomyL3 }

// Rule is an allow/deny pattern list. Deny always wins; an empty Allow allows nothing (default deny).
type Rule struct {
	Allow []string `json:"allow,omitempty"`
	Deny  []string `json:"deny,omitempty"`
}

// Policy is the capability policy bound to an agent. It is evaluated by the control plane for every
// tool request and, as a signed bundle, by the agent before asking.
type Policy struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
	// Tools by name (glob).
	Tools Rule `json:"tools"`
	// Paths the file tools and shell cwd may touch. "$WORKDIR" expands to the agent workdir; "**"
	// matches across directories.
	Paths Rule `json:"paths"`
	// Commands a shell call may run, matched against the whole command line; "*" matches anything.
	Commands Rule `json:"commands"`
	// Domains http_fetch may reach; "*.example.com" matches subdomains.
	Domains Rule `json:"domains"`
	// Services (systemd units) the service tools may touch, e.g. "nginx.service", "app-*".
	Services Rule `json:"services,omitempty"`
	// Containers (names) the docker tools may touch.
	Containers Rule `json:"containers,omitempty"`
	// Apps (Miabi application names) the Miabi tools may touch.
	Apps Rule `json:"apps,omitempty"`
	// Terminal allows operators to open a recorded interactive terminal on the agent.
	Terminal bool `json:"terminal,omitempty"`
	// MaxRisk caps what this agent may ever do, even with approval.
	MaxRisk Risk `json:"max_risk"`
	// RequireApproval lists tools (glob) that always need a human, regardless of autonomy.
	RequireApproval []string `json:"require_approval,omitempty"`
	// AllowShellMeta permits ; & | ` $( < > and newlines in shell commands. Off by default: with
	// metacharacters, an allowed prefix like "ls *" would also match "ls; rm -rf /".
	AllowShellMeta bool `json:"allow_shell_meta,omitempty"`
}

// Call is a tool call to evaluate.
type Call struct {
	Tool  string
	Input json.RawMessage
}

// Decision is the policy outcome for a call.
type Decision struct {
	Effect    string    `json:"effect"` // EffectAllow | EffectDeny | EffectApprove
	Reason    string    `json:"reason"`
	Risk      Risk      `json:"risk"`
	Resources Resources `json:"resources"`
}

func deny(risk Risk, res Resources, format string, a ...any) Decision {
	return Decision{Effect: EffectDeny, Reason: fmt.Sprintf(format, a...), Risk: risk, Resources: res}
}

// Evaluate decides a call under a policy, autonomy level and workdir. It is pure and deterministic,
// so the agent and the control plane reach the same answer for the same inputs.
func Evaluate(p Policy, autonomy Autonomy, workdir string, call Call) Decision {
	return EvaluateAt(p, autonomy, workdir, workdir, call)
}

// EvaluateAt is Evaluate with relative paths resolved against base (a project worktree inside the
// workdir) while "$WORKDIR" in the policy still means the agent workdir.
func EvaluateAt(p Policy, autonomy Autonomy, workdir, base string, call Call) Decision {
	spec, ok := LookupTool(call.Tool)
	if !ok {
		return deny(0, Resources{}, "unknown tool %q", call.Tool)
	}
	res, err := spec.Resources(call.Input)
	if err != nil {
		return deny(spec.Risk, res, "%s: %v", call.Tool, err)
	}
	if !matchRule(p.Tools, call.Tool, globMatch) {
		return deny(spec.Risk, res, "tool %q is not allowed by policy %q", call.Tool, p.Name)
	}
	if p.MaxRisk == 0 || spec.Risk > p.MaxRisk {
		return deny(spec.Risk, res, "tool %q is %s risk; policy %q allows at most %s", call.Tool, spec.Risk, p.Name, p.MaxRisk)
	}
	for _, raw := range res.Paths {
		abs := ResolvePath(base, raw)
		if !matchRule(expandWorkdir(p.Paths, workdir), abs, pathMatch) {
			return deny(spec.Risk, res, "path %q is not allowed by policy %q", abs, p.Name)
		}
	}
	for _, cmd := range res.Commands {
		if !p.AllowShellMeta && hasShellMeta(cmd) {
			return deny(spec.Risk, res, "shell metacharacters are not allowed by policy %q", p.Name)
		}
		if !matchRule(p.Commands, strings.TrimSpace(cmd), commandMatch) {
			return deny(spec.Risk, res, "command is not allowed by policy %q", p.Name)
		}
	}
	for _, d := range res.Domains {
		if !matchRule(p.Domains, d, domainMatch) {
			return deny(spec.Risk, res, "domain %q is not allowed by policy %q", d, p.Name)
		}
	}
	for _, u := range res.Services {
		if !matchRule(p.Services, u, globMatch) {
			return deny(spec.Risk, res, "service %q is not allowed by policy %q", u, p.Name)
		}
	}
	for _, a := range res.Apps {
		if !matchRule(p.Apps, a, globMatch) {
			return deny(spec.Risk, res, "Miabi app %q is not allowed by policy %q", a, p.Name)
		}
	}
	for _, c := range res.Containers {
		if !matchRule(p.Containers, c, globMatch) {
			return deny(spec.Risk, res, "container %q is not allowed by policy %q", c, p.Name)
		}
	}

	d := Decision{Effect: EffectAllow, Risk: spec.Risk, Resources: res}
	switch {
	case spec.Risk >= RiskCritical:
		d.Effect, d.Reason = EffectApprove, "critical actions always require approval"
	case anyMatch(p.RequireApproval, call.Tool, globMatch):
		d.Effect, d.Reason = EffectApprove, fmt.Sprintf("policy %q requires approval for %s", p.Name, call.Tool)
	case spec.Risk > autonomy.autoMax():
		d.Effect, d.Reason = EffectApprove, fmt.Sprintf("%s risk exceeds autonomy L%d", spec.Risk, autonomy)
	default:
		d.Reason = fmt.Sprintf("allowed by policy %q", p.Name)
	}
	return d
}

// ResolvePath makes p absolute against workdir and cleans it, so "../" cannot escape a pattern.
func ResolvePath(workdir, p string) string {
	if !path.IsAbs(p) {
		p = path.Join(workdir, p)
	}
	return path.Clean(p)
}

func expandWorkdir(r Rule, workdir string) Rule {
	ex := func(in []string) []string {
		out := make([]string, len(in))
		for i, s := range in {
			out[i] = strings.ReplaceAll(s, "$WORKDIR", strings.TrimRight(workdir, "/"))
		}
		return out
	}
	return Rule{Allow: ex(r.Allow), Deny: ex(r.Deny)}
}

func matchRule(r Rule, s string, match func(pattern, s string) bool) bool {
	if anyMatch(r.Deny, s, match) {
		return false
	}
	return anyMatch(r.Allow, s, match)
}

func anyMatch(patterns []string, s string, match func(pattern, s string) bool) bool {
	for _, p := range patterns {
		if match(p, s) {
			return true
		}
	}
	return false
}

func hasShellMeta(cmd string) bool {
	return strings.ContainsAny(cmd, ";&|`<>\n\r") || strings.Contains(cmd, "$(")
}

// globMatch: "*" matches any run of characters.
func globMatch(pattern, s string) bool { return wildcard(pattern, s, false) }

// commandMatch: "*" matches anything, including spaces and slashes.
func commandMatch(pattern, s string) bool { return wildcard(strings.TrimSpace(pattern), s, false) }

// domainMatch: exact, or "*.example.com" for subdomains (not the apex), or "*" for any.
func domainMatch(pattern, host string) bool {
	pattern = strings.ToLower(pattern)
	if pattern == "*" || pattern == host {
		return true
	}
	if strings.HasPrefix(pattern, "*.") {
		return strings.HasSuffix(host, pattern[1:])
	}
	return false
}

// pathMatch: "**" matches across "/", "*" and "?" stay within one segment.
func pathMatch(pattern, p string) bool { return wildcard(path.Clean(pattern), p, true) }

// wildcard matches with * and ?; when segmented, * and ? stop at "/" and ** crosses it.
// A trailing "/**" also matches the directory itself.
func wildcard(pattern, s string, segmented bool) bool {
	if segmented && strings.HasSuffix(pattern, "/**") && s == strings.TrimSuffix(pattern, "/**") {
		return true
	}
	return wild(pattern, s, segmented)
}

func wild(p, s string, seg bool) bool {
	for len(p) > 0 {
		switch {
		case strings.HasPrefix(p, "**") && seg:
			rest := strings.TrimLeft(p, "*")
			for i := 0; i <= len(s); i++ {
				if wild(rest, s[i:], seg) {
					return true
				}
			}
			return false
		case p[0] == '*':
			rest := p[1:]
			for i := 0; i <= len(s); i++ {
				if wild(rest, s[i:], seg) {
					return true
				}
				if i < len(s) && seg && s[i] == '/' {
					return false
				}
			}
			return false
		case p[0] == '?':
			if len(s) == 0 || (seg && s[0] == '/') {
				return false
			}
			p, s = p[1:], s[1:]
		default:
			if len(s) == 0 || p[0] != s[0] {
				return false
			}
			p, s = p[1:], s[1:]
		}
	}
	return len(s) == 0
}

// SignedPolicy is a policy bundle signed by the control plane. The agent verifies it against the key
// pinned at enrollment before enforcing it.
type SignedPolicy struct {
	Policy    json.RawMessage `json:"policy"`
	Signature []byte          `json:"signature"`
}

// SignPolicy serialises and signs a policy.
func SignPolicy(p Policy, key ed25519.PrivateKey) (SignedPolicy, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return SignedPolicy{}, err
	}
	return SignedPolicy{Policy: b, Signature: ed25519.Sign(key, b)}, nil
}

// ErrBadSignature means a policy bundle was not signed by the pinned control-plane key.
var ErrBadSignature = errors.New("policy signature verification failed")

// Verify checks the signature and returns the policy.
func (s SignedPolicy) Verify(pub ed25519.PublicKey) (Policy, error) {
	var p Policy
	if len(pub) != ed25519.PublicKeySize || !ed25519.Verify(pub, s.Policy, s.Signature) {
		return p, ErrBadSignature
	}
	if err := json.Unmarshal(s.Policy, &p); err != nil {
		return p, err
	}
	return p, nil
}

// PathAllowed reports whether the policy's path rule admits an absolute path. Agents call it again
// on the real path after resolving symlinks, so a link inside the workdir cannot reach a denied path.
func (p Policy) PathAllowed(workdir, abs string) bool {
	return matchRule(expandWorkdir(p.Paths, workdir), ResolvePath(workdir, abs), pathMatch)
}

// ProjectDir is where an agent keeps a project's worktree for a branch. Both sides compute it so the
// control plane resolves relative paths exactly as the agent will.
func ProjectDir(workdir, slug, branch string) string {
	return path.Join(workdir, "projects", slug, strings.ReplaceAll(branch, "/", "-"))
}

// ProjectMirror is the bare repository shared by a project's worktrees.
func ProjectMirror(workdir, slug string) string {
	return path.Join(workdir, "projects", slug, ".mirror.git")
}

// AllowsTool reports whether the policy lets the tool be called at all (by name and risk ceiling).
// Individual calls are still evaluated in full; this only decides which tools to offer a model.
func (p Policy) AllowsTool(name string) bool {
	spec, ok := LookupTool(name)
	return ok && spec.Risk <= p.MaxRisk && matchRule(p.Tools, name, globMatch)
}
