// Copyright (c) 2026 Jonas Kaninda.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package proto

import (
	"regexp"
	"sort"
	"strings"
)

const Redacted = "[REDACTED]"

// secretPatterns are unambiguous enough to redact anywhere; tool output would otherwise reach the LLM provider.
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

func secretEnvName(name string) bool {
	n := strings.ToUpper(name)
	for _, s := range []string{"TOKEN", "SECRET", "PASSWORD", "PASSWD", "PASS", "KEY", "CREDENTIAL", "AUTH", "COOKIE", "SESSION", "DSN"} {
		if strings.Contains(n, s) {
			return true
		}
	}
	return false
}

func SecretsFromEnv(env []string) []string {
	var out []string
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && secretEnvName(k) {
			out = append(out, v)
		}
	}
	return out
}

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
