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
