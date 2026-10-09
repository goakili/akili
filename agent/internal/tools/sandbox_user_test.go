// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package tools

import (
	"os"
	"strconv"
	"testing"

	"github.com/goakili/akili/proto"
)

func TestSandboxUser(t *testing.T) {
	root := &Project{Spec: proto.ProjectSpec{SandboxRoot: true}, Dir: t.TempDir(), Cache: t.TempDir()}
	if u, _ := sandboxUser(root); u != "0:0" {
		t.Fatalf("sandbox_root: user = %q, want 0:0", u)
	}
	if os.Getuid() == 0 {
		t.Skip("the unprivileged case needs a non-root test runner")
	}
	p := &Project{Dir: t.TempDir(), Cache: t.TempDir()}
	want := strconv.Itoa(os.Getuid()) + ":" + strconv.Itoa(os.Getgid())
	if u, restore := sandboxUser(p); u != want {
		t.Fatalf("user = %q, want the agent's own %q", u, want)
	} else {
		restore()
	}
}
