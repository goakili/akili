// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package procsec

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

const secret = "AKILI_TEST_SECRET=must-not-leak"

// TestMain doubles as the helper process: it optionally hardens itself, then lets a same-user
// child try to read its environment, as a stdio MCP server could.
func TestMain(m *testing.M) {
	if mode := os.Getenv("AKILI_NODUMP_HELPER"); mode != "" {
		if mode == "on" {
			if err := NoDump(); err != nil {
				os.Exit(2)
			}
		}
		out, _ := exec.Command("sh", "-c", `cat /proc/$PPID/environ | tr '\0' '\n'`).Output()
		if strings.Contains(string(out), secret) {
			os.Stdout.WriteString("readable")
		} else {
			os.Stdout.WriteString("hidden")
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func helper(t *testing.T, mode string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = []string{"AKILI_NODUMP_HELPER=" + mode, secret, "PATH=" + os.Getenv("PATH")}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("helper %s: %v", mode, err)
	}
	return string(out)
}

func TestNoDumpHidesEnvironFromChildren(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root may read any process's /proc files")
	}
	if got := helper(t, "off"); got != "readable" {
		t.Skipf("this kernel already hides same-user environ (%s); nothing to prove", got)
	}
	if got := helper(t, "on"); got != "hidden" {
		t.Fatalf("a same-user child read the hardened process's environment (%s)", got)
	}
}
