// Copyright (c) 2026 Jonas Kaninda.
//
// This file is part of Akili.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.
//
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
// child try to read its environment, as a shell tool could.
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
