// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: Apache-2.0

package proto

import (
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	cases := []struct{ in, leak string }{
		{"token=ghp_0123456789abcdefghijklmnopqrstuvwxyzAB", "ghp_0123456789"},
		{"AWS_ACCESS_KEY_ID=AKIAABCDEFGHIJKLMNOP", "AKIAABCDEFGHIJKLMNOP"},
		{"-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA\n-----END OPENSSH PRIVATE KEY-----", "b3BlbnNzaC1rZXktdjEAAAAA"},
		{"curl -H 'Authorization: Bearer abc.def.ghi-secret'", "abc.def.ghi-secret"},
		{"postgres://akili:hunter2hunter2@db:5432/akili", "hunter2hunter2"},
		{"key sk-ant-api03-ABCDEFGHIJKLMNOPQRSTUVWXYZ012345", "sk-ant-api03"},
		{"GITLAB_TOKEN=glpat-AbCdEfGhIjKlMnOpQrSt12", "glpat-AbCdEfGh"},
		{"routable glpat-AbCdEfGhIjKlMnOpQrSt12.01.0x1k2c3d4 token", ".01.0x1k2c3d4"},
		{"deploy gldt-AbCdEfGhIjKlMnOpQrSt12", "gldt-AbCdEfGh"},
		{"runner glrt-t1_AbCdEfGhIjKlMnOpQrSt", "glrt-t1_AbCdEf"},
		{"trigger glptt-0123456789abcdef0123456789abcdef01234567", "glptt-0123456789"},
		{"job glcbt-64_AbCdEfGhIjKlMnOpQrSt", "glcbt-64_AbCdEf"},
		{"oauth gloas-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", "gloas-0123456789"},
		{"join akj_ABCDEFGHIJKLMNOPQRSTUVWXYZ234567", "akj_ABCDEFGHIJ"},
		{"miabi mb_ABCDEFGHIJKLMNOPQRSTUVWX", "mb_ABCDEFGHIJ"},
		{"jwt eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ1c3JfMSJ9.c2lnbmF0dXJlLXNpZ25hdHVyZQ", "eyJzdWIiOiJ1c3JfMSJ9"},
	}
	for _, c := range cases {
		if out := Redact(c.in); strings.Contains(out, c.leak) || !strings.Contains(out, Redacted) {
			t.Errorf("Redact(%q) = %q", c.in, out)
		}
	}
	if out := Redact("value is akili-test-secret-DO-NOT-USE here", "akili-test-secret-DO-NOT-USE"); strings.Contains(out, "DO-NOT-USE") {
		t.Errorf("explicit secret leaked: %q", out)
	}
	if out := Redact("df -h: /dev/sda1 50% used, key=abc"); out != "df -h: /dev/sda1 50% used, key=abc" {
		t.Errorf("ordinary output changed: %q", out)
	}
	env := SecretsFromEnv([]string{"PATH=/usr/bin", "KUBECONFIG=/etc/kube", "VAULT_TOKEN=s.abcdef123", "DB_PASSWORD=pw123456"})
	if len(env) != 2 {
		t.Errorf("secret env values = %v", env)
	}
}
