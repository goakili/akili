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

package procsec

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

const secret = "AKILI_TEST_SECRET=must-not-leak"

// TestMain doubles as the helper process: it optionally hardens itself, then lets a same-user child try to read its environment.
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
