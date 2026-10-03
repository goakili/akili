// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package gitid resolves and validates the git identity an agent commits under. The control plane
// owns it: it is sent to the agent in the project spec, and the git proxy refuses pushed commits
// that carry any other author or committer.
package gitid

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/goakili/akili/server/internal/models"
)

// Defaults for agents without their own identity. The email is verified on the akili-agent GitHub
// account, so commits link to it.
const (
	DefaultNameTemplate  = "Akili Agent"
	DefaultEmailTemplate = "agent@goakili.dev"
)

// Config is the server-wide identity setup.
type Config struct {
	// NameTemplate is the name of agents without their own; {agent_id} and {agent_name} expand.
	NameTemplate string
	// EmailTemplate is the email of agents without their own; {agent_id} and {agent_name} expand.
	EmailTemplate string
	// AllowedDomains restricts the emails an admin may give an agent; empty allows any domain.
	AllowedDomains []string
}

// Identity is a git author/committer.
type Identity = models.GitIdentity

// For returns the identity agent a commits under.
func (c Config) For(a *models.Agent) Identity {
	id := Identity{Name: a.GitName, Email: a.GitEmail}
	if id.Name == "" {
		id.Name = c.renderName(a.ID, a.Name)
	}
	if id.Email == "" {
		id.Email = c.renderEmail(a.ID, a.Name)
	}
	return id
}

func (c Config) renderName(agentID, agentName string) string {
	t := c.NameTemplate
	if t == "" {
		t = DefaultNameTemplate
	}
	return strings.NewReplacer("{agent_id}", agentID, "{agent_name}", cleanName(agentName)).Replace(t)
}

func (c Config) renderEmail(agentID, agentName string) string {
	t := c.EmailTemplate
	if t == "" {
		t = DefaultEmailTemplate
	}
	return strings.NewReplacer("{agent_id}", agentID, "{agent_name}", slug(agentName)).Replace(t)
}

// Validate checks the templates and the domain list.
func (c Config) Validate() error {
	if err := ValidateName(c.renderName("ag_example", "example")); err != nil {
		return fmt.Errorf("AKILI_GIT_NAME_TEMPLATE: %w", err)
	}
	if err := ValidateEmail(c.renderEmail("ag_example", "example"), nil); err != nil {
		return fmt.Errorf("AKILI_GIT_EMAIL_TEMPLATE: %w", err)
	}
	for _, d := range c.AllowedDomains {
		if d == "" || strings.ContainsAny(d, "@ \t") {
			return fmt.Errorf("AKILI_GIT_EMAIL_DOMAINS: %q is not a domain", d)
		}
	}
	return nil
}

// ValidateEmail accepts a bare address (no display name, no quoting) whose domain is in allowed, if set.
// The value ends up in git config and commit headers, so anything git would reinterpret is refused.
func ValidateEmail(email string, allowed []string) error {
	if email == "" || len(email) > 254 {
		return errors.New("git email must be 1-254 characters")
	}
	if strings.IndexFunc(email, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) || r == '<' || r == '>' }) >= 0 {
		return errors.New("git email must not contain spaces, control characters, < or >")
	}
	local, domain, ok := strings.Cut(email, "@")
	if !ok || !validLocal(local) || !validDomain(domain) {
		return errors.New("git email is not a valid address")
	}
	if len(allowed) == 0 {
		return nil
	}
	for _, d := range allowed {
		if strings.EqualFold(d, domain) {
			return nil
		}
	}
	return fmt.Errorf("git email domain must be one of: %s", strings.Join(allowed, ", "))
}

// validLocal allows RFC 5322 atom characters plus [ and ], which GitHub App bot addresses use
// (123+app[bot]@users.noreply.github.com).
func validLocal(s string) bool {
	if s == "" || len(s) > 64 || s[0] == '.' || s[len(s)-1] == '.' || strings.Contains(s, "..") {
		return false
	}
	for _, r := range s {
		if !(r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)) || strings.ContainsRune("!#$%&'*+-/=?^_`{|}~.[]", r)) {
			return false
		}
	}
	return true
}

func validDomain(s string) bool {
	labels := strings.Split(s, ".")
	if len(labels) < 2 {
		return false
	}
	for _, l := range labels {
		if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for _, r := range l {
			if !(r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)) || r == '-') {
				return false
			}
		}
	}
	return true
}

// ValidateName accepts a display name git can store unchanged.
func ValidateName(name string) error {
	if strings.TrimSpace(name) != name || name == "" || len(name) > 120 {
		return errors.New("git name must be 1-120 characters without surrounding spaces")
	}
	if strings.IndexFunc(name, func(r rune) bool { return unicode.IsControl(r) || r == '<' || r == '>' }) >= 0 {
		return errors.New("git name must not contain control characters, < or >")
	}
	return nil
}

// ValidateForgeLogin accepts a GitHub or Gitea username, which is written into pull request bodies.
func ValidateForgeLogin(login string) error {
	if login == "" || len(login) > 64 || !isAlnum(rune(login[0])) {
		return errors.New("forge username must be 1-64 letters, digits, '.', '_' or '-', starting with a letter or digit")
	}
	for _, r := range login {
		if !isAlnum(r) && !strings.ContainsRune("._-", r) {
			return errors.New("forge username must be 1-64 letters, digits, '.', '_' or '-', starting with a letter or digit")
		}
	}
	return nil
}

// AgentTrailer names the agent that made a commit.
func AgentTrailer(a *models.Agent) string {
	return "Akili-Agent: " + cleanName(a.Name)
}

// CoAuthor is the Co-Authored-By trailer crediting u, or "" when u has not opted in.
func CoAuthor(u *models.User) string {
	if u == nil || u.CoAuthorEmail == "" {
		return ""
	}
	name := strings.TrimSpace(cleanName(u.Name))
	if name == "" {
		name = u.ForgeLogin
	}
	if name == "" {
		name, _, _ = strings.Cut(u.CoAuthorEmail, "@")
	}
	return "Co-Authored-By: " + name + " <" + u.CoAuthorEmail + ">"
}

func isAlnum(r rune) bool { return r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)) }

// cleanName drops what git strips or refuses in a name, since agent names are not restricted.
func cleanName(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '<' || r == '>' {
			return -1
		}
		return r
	}, s)
}

func slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if out == "" {
		return "agent"
	}
	return out
}
