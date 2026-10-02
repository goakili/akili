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

// DefaultEmailTemplate uses a reserved domain (RFC 2606), so no forge account can claim the commits.
const DefaultEmailTemplate = "akili+{agent_id}@akili.invalid"

// Config is the server-wide identity setup.
type Config struct {
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
		id.Name = "Akili (" + cleanName(a.Name) + ")"
	}
	if id.Email == "" {
		id.Email = c.render(a.ID, a.Name)
	}
	return id
}

func (c Config) render(agentID, agentName string) string {
	t := c.EmailTemplate
	if t == "" {
		t = DefaultEmailTemplate
	}
	return strings.NewReplacer("{agent_id}", agentID, "{agent_name}", slug(agentName)).Replace(t)
}

// Validate checks the template and the domain list.
func (c Config) Validate() error {
	if err := ValidateEmail(c.render("ag_example", "example"), nil); err != nil {
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
