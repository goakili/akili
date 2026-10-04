// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
)

func TestVerifyPKCE(t *testing.T) {
	verifier := strings.Repeat("v", 43)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	if !VerifyPKCE(verifier, challenge) {
		t.Fatal("matching verifier refused")
	}
	if VerifyPKCE(verifier+"x", challenge) || VerifyPKCE(verifier, strings.Repeat("A", 43)) {
		t.Fatal("wrong verifier or challenge accepted")
	}
}

func TestVSCodeRedirect(t *testing.T) {
	state := "0123456789abcdef"
	got, err := VSCodeRedirect("vscode", "", "akc_x", state)
	if err != nil || got != "vscode://goakili.akili/callback?code=akc_x&state="+state {
		t.Fatalf("got %q, %v", got, err)
	}
	got, err = VSCodeRedirect("cursor", "5", "akc_x", state)
	if err != nil || got != "cursor://goakili.akili/callback?code=akc_x&state="+state+"&windowId=5" {
		t.Fatalf("window: got %q, %v", got, err)
	}
	for _, bad := range [][2]string{
		{"https", ""}, {"javascript", ""}, {"", ""}, {"VSCODE", ""}, {"vscode://goakili.akili/callback", ""},
		{"vscode", "x"}, {"vscode", "5&code=forged"}, {"vscode", "12345678901"},
	} {
		if _, err := VSCodeRedirect(bad[0], bad[1], "akc_x", state); err == nil {
			t.Errorf("editor %q window %q accepted", bad[0], bad[1])
		}
	}
	for _, bad := range []string{"", "short", "has space 0123456", strings.Repeat("a", 129)} {
		if _, err := VSCodeRedirect("vscode", "", "akc_x", bad); err == nil {
			t.Errorf("state %q accepted", bad)
		}
	}
}

func TestValidateClientName(t *testing.T) {
	for name, ok := range map[string]bool{
		"VS Code on laptop": true, "": false, " padded": false, "line\nbreak": false, strings.Repeat("a", 81): false,
	} {
		if err := ValidateClientName(name); (err == nil) != ok {
			t.Errorf("%q: ok=%v err=%v", name, err == nil, err)
		}
	}
}
