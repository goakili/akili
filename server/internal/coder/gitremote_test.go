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

package coder

import "testing"

func TestParseGitRemote(t *testing.T) {
	want := GitRemote{Host: "github.com", Owner: "akili-agent", Repo: "simple-api", Path: "akili-agent"}
	for _, raw := range []string{
		"https://github.com/akili-agent/simple-api",
		"https://github.com/akili-agent/simple-api.git",
		"https://github.com/akili-agent/simple-api/",
		"https://user:token@GitHub.com/akili-agent/simple-api.git",
		"git@github.com:akili-agent/simple-api.git",
		"ssh://git@github.com:22/akili-agent/simple-api.git",
		" git@github.com:akili-agent/simple-api \n",
	} {
		got, err := ParseGitRemote(raw)
		if err != nil || got != want {
			t.Errorf("%q: got %+v, %v", raw, got, err)
		}
	}
	got, err := ParseGitRemote("https://git.example.com:3000/gitea/team/svc.git")
	if err != nil || got != (GitRemote{Host: "git.example.com", Owner: "team", Repo: "svc", Path: "gitea/team"}) {
		t.Errorf("path prefix: got %+v, %v", got, err)
	}
	got, err = ParseGitRemote("git@gitlab.com:platform/backend/inventory-api.git")
	if err != nil || got.Path != "platform/backend" || got.Repo != "inventory-api" {
		t.Errorf("nested group: got %+v, %v", got, err)
	}
	for _, bad := range []string{"", "/home/me/repo", "file:///srv/repo.git", "https://github.com/only-owner", "ftp://x/a/b", "github.com:a/b"} {
		if _, err := ParseGitRemote(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
