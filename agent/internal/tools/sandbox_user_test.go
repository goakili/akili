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
