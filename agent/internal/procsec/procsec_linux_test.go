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

package procsec

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

const secret = "AKILI_TEST_SECRET=must-not-leak"

// TestMain doubles as the helper process whose environment a same-user child tries to read.
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
