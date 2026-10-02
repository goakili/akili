// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package coder

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
)

func pkt(s string) string { return fmt.Sprintf("%04x%s", len(s)+4, s) }

const (
	oldSHA = "1111111111111111111111111111111111111111"
	newSHA = "2222222222222222222222222222222222222222"
	branch = "akili/tsk_abc"
)

func push(cmds ...string) string {
	var b strings.Builder
	for i, c := range cmds {
		if i == 0 {
			c += "\x00report-status side-band-64k"
		}
		b.WriteString(pkt(c + "\n"))
	}
	b.WriteString("0000")
	b.WriteString("PACK\x00\x00\x00\x02rest-of-pack-data")
	return b.String()
}

func TestPushGuard(t *testing.T) {
	tests := []struct {
		name string
		body string
		ok   bool
	}{
		{"update own branch", push(oldSHA + " " + newSHA + " refs/heads/" + branch), true},
		{"create own branch", push(zeroSHA + " " + newSHA + " refs/heads/" + branch), true},
		{"push to main refused", push(oldSHA + " " + newSHA + " refs/heads/main"), false},
		{"push to another akili branch refused", push(oldSHA + " " + newSHA + " refs/heads/akili/other"), false},
		{"delete own branch refused", push(oldSHA + " " + zeroSHA + " refs/heads/" + branch), false},
		{"tag refused", push(zeroSHA + " " + newSHA + " refs/tags/v1"), false},
		{"own branch plus main in one push refused", push(oldSHA+" "+newSHA+" refs/heads/"+branch, oldSHA+" "+newSHA+" refs/heads/main"), false},
		{"signed push refused", pkt("push-cert\x00report-status\n") + "0000", false},
		{"garbage refused", "zzzz", false},
		{"empty refused", "0000", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, cmds, err := readPushCommands(strings.NewReader(tt.body))
			if err == nil {
				err = checkPush(cmds, branch)
			}
			if (err == nil) != tt.ok {
				t.Fatalf("ok=%v, err=%v", err == nil, err)
			}
		})
	}
}

// TestPushReplayIsLossless: what goes upstream must be byte-identical to what the agent sent.
func TestPushReplayIsLossless(t *testing.T) {
	body := push(oldSHA + " " + newSHA + " refs/heads/" + branch)
	r := strings.NewReader(body)
	prefix, packHead, _, err := readPushCommands(r)
	if err != nil {
		t.Fatal(err)
	}
	replayed, _ := io.ReadAll(io.MultiReader(bytes.NewReader(prefix), bytes.NewReader(packHead), r))
	if string(replayed) != body {
		t.Fatalf("replay differs:\n got %q\nwant %q", replayed, body)
	}
}

func TestShallowLinesAreSkipped(t *testing.T) {
	body := pkt("shallow "+oldSHA+"\n") + pkt(oldSHA+" "+newSHA+" refs/heads/"+branch+"\x00report-status\n") + "0000"
	_, _, cmds, err := readPushCommands(strings.NewReader(body))
	if err != nil || len(cmds) != 1 || checkPush(cmds, branch) != nil {
		t.Fatalf("cmds=%v err=%v", cmds, err)
	}
}

func TestBranchFor(t *testing.T) {
	if BranchFor("tsk_1", "ses_1") != "akili/tsk_1" || BranchFor("", "ses_1") != "akili/ses_1" {
		t.Fatal("branch naming changed")
	}
}
