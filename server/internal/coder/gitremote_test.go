// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

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
