// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: Apache-2.0

package proto

import (
	"regexp"
	"sort"
	"strings"
)

// Redacted replaces secrets in tool output.
const Redacted = "[REDACTED]"

// secretPatterns match credential formats that are unambiguous enough to redact wherever they
// appear. Tool output enters the model's context, so a leaked token would reach the LLM provider.
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`),
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),                                                    // AWS access key id
	regexp.MustCompile(`\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{36,}\b`),                            // GitHub tokens
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{22,}\b`),                                        // GitHub fine-grained
	regexp.MustCompile(`\bgl(?:pat|dt|rt|ptt|cbt|oas)-[A-Za-z0-9_\-]{20,}(?:\.[A-Za-z0-9_\-]+)*`), // GitLab access, deploy, runner, trigger, CI job, OAuth
	regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}\b`),                                        // Slack
	regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_\-]{20,}\b`),                                          // Anthropic
	regexp.MustCompile(`\bsk-(?:proj-)?[A-Za-z0-9_\-]{32,}\b`),                                    // OpenAI-style
	regexp.MustCompile(`\b(?:akj|ak|mb)_[A-Za-z0-9]{20,}\b`),                                      // Akili join/API keys, Miabi keys
	regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]{10,}\.eyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\b`), // JWT
	regexp.MustCompile(`(?i)(authorization:\s*(?:bearer|basic|token)\s+)[^\s"']+`),
	regexp.MustCompile(`(?i)(://[^/\s:@]+:)[^@\s/]+(@)`), // credentials in URLs
}

// secretEnvName reports whether an environment variable name suggests a secret value.
func secretEnvName(name string) bool {
	n := strings.ToUpper(name)
	for _, s := range []string{"TOKEN", "SECRET", "PASSWORD", "PASSWD", "PASS", "KEY", "CREDENTIAL", "AUTH", "COOKIE", "SESSION", "DSN"} {
		if strings.Contains(n, s) {
			return true
		}
	}
	return false
}

// SecretsFromEnv returns the values of KEY=value pairs whose names look secret, for Redact.
func SecretsFromEnv(env []string) []string {
	var out []string
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && secretEnvName(k) {
			out = append(out, v)
		}
	}
	return out
}

// Redact removes known secret values (exact matches, 6+ characters) and well-known credential
// formats from s.
func Redact(s string, secrets ...string) string {
	if s == "" {
		return s
	}
	vals := make([]string, 0, len(secrets))
	for _, v := range secrets {
		if len(v) >= 6 {
			vals = append(vals, v)
		}
	}
	// Longest first so a secret containing another is replaced whole.
	sort.Slice(vals, func(i, j int) bool { return len(vals[i]) > len(vals[j]) })
	for _, v := range vals {
		s = strings.ReplaceAll(s, v, Redacted)
	}
	for _, re := range secretPatterns {
		if re.NumSubexp() == 2 {
			s = re.ReplaceAllString(s, "${1}"+Redacted+"${2}")
		} else if re.NumSubexp() == 1 {
			s = re.ReplaceAllString(s, "${1}"+Redacted)
		} else {
			s = re.ReplaceAllString(s, Redacted)
		}
	}
	return s
}
