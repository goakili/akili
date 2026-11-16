// Copyright (c) 2026 Jonas Kaninda.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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
