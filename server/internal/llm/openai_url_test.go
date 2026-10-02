// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package llm

import "testing"

func TestNormalizeBaseURL(t *testing.T) {
	for in, want := range map[string]string{
		"":                               "https://api.openai.com/v1",
		"http://localhost:11434":         "http://localhost:11434/v1",
		"http://localhost:11434/":        "http://localhost:11434/v1",
		"http://localhost:11434/v1":      "http://localhost:11434/v1",
		"https://example.com/openai/v1/": "https://example.com/openai/v1",
	} {
		if got := normalizeBaseURL(in); got != want {
			t.Errorf("normalizeBaseURL(%q) = %q, want %q", in, got, want)
		}
	}
}
