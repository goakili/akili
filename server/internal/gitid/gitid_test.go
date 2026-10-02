// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package gitid

import (
	"testing"

	"github.com/goakili/akili/server/internal/models"
)

func TestFor(t *testing.T) {
	a := &models.Agent{Base: models.Base{ID: "ag_1"}, Name: "Web <01>\n"}
	if got := (Config{}).For(a); got.Name != "Akili (Web 01)" || got.Email != "akili+ag_1@akili.invalid" {
		t.Fatalf("default: %v", got)
	}
	if got := (Config{EmailTemplate: "{agent_name}@agents.example.com"}).For(a); got.Email != "web-01@agents.example.com" {
		t.Fatalf("template: %v", got)
	}
	a.GitName, a.GitEmail = "Builder", "builder@example.com"
	if got := (Config{}).For(a); got.String() != "Builder <builder@example.com>" {
		t.Fatalf("override: %v", got)
	}
}

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		email   string
		allowed []string
		ok      bool
	}{
		{"bot@example.com", nil, true},
		{"123+akili[bot]@users.noreply.github.com", nil, true},
		{"bot@Example.com", []string{"example.com"}, true},
		{"bot@other.com", []string{"example.com"}, false},
		{"bot@sub.example.com", []string{"example.com"}, false},
		{"", nil, false},
		{"not-an-email", nil, false},
		{"Bot <bot@example.com>", nil, false},
		{"bot@example.com\n[core]", nil, false},
		{"bot@example.com>", nil, false},
		{"b ot@example.com", nil, false},
		{"bot@example.com,ceo@example.com", nil, false},
		{"bot@localhost", nil, false},
		{"a@b@example.com", nil, false},
		{".bot@example.com", nil, false},
		{"bot@exa_mple.com", nil, false},
		{`"quoted"@example.com`, nil, false},
	}
	for _, tt := range tests {
		if err := ValidateEmail(tt.email, tt.allowed); (err == nil) != tt.ok {
			t.Errorf("%q allowed=%v: ok=%v err=%v", tt.email, tt.allowed, err == nil, err)
		}
	}
}

func TestValidateName(t *testing.T) {
	for name, ok := range map[string]bool{
		"Akili Builder": true,
		"":              false,
		" padded":       false,
		"a <b>":         false,
		"line\nbreak":   false,
	} {
		if err := ValidateName(name); (err == nil) != ok {
			t.Errorf("%q: ok=%v err=%v", name, err == nil, err)
		}
	}
}

func TestConfigValidate(t *testing.T) {
	if err := (Config{EmailTemplate: DefaultEmailTemplate}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (Config{EmailTemplate: "{agent_id}"}).Validate(); err == nil {
		t.Fatal("template without a domain accepted")
	}
	if err := (Config{AllowedDomains: []string{"a@b"}}).Validate(); err == nil {
		t.Fatal("bad domain accepted")
	}
}
