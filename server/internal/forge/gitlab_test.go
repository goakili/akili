// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package forge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// gitlabFake answers by escaped path (RawPath keeps %2F), and records every request it saw.
func gitlabFake(t *testing.T, routes map[string]func(w http.ResponseWriter, r *http.Request)) (*GitLab, *[]string) {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer glpat-test" {
			http.Error(w, `{"message":"401 Unauthorized"}`, http.StatusUnauthorized)
			return
		}
		key := r.Method + " " + r.URL.EscapedPath()
		seen = append(seen, key+"?"+r.URL.RawQuery)
		if h, ok := routes[key]; ok {
			h(w, r)
			return
		}
		http.Error(w, `{"message":"404 Not found"}`, http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	g, err := NewGitLab(srv.URL+"/", "", "glpat-test", "")
	if err != nil {
		t.Fatal(err)
	}
	return g, &seen
}

func reply(body string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) }
}

func TestGitLabNestedGroupPaths(t *testing.T) {
	const proj = "/api/v4/projects/platform%2Fbackend%2Finventory-api"
	g, seen := gitlabFake(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET " + proj: reply(`{"path":"inventory-api","default_branch":"main","web_url":"https://gl/x","visibility":"internal","namespace":{"full_path":"platform/backend"}}`),
		"GET " + proj + "/repository/branches/akili%2Ftsk_1":  reply(`{"commit":{"id":"abc123"}}`),
		"GET " + proj + "/repository/commits/abc123/statuses": reply(`[]`),
	})
	repo, err := g.GetRepo(context.Background(), "platform/backend", "inventory-api")
	if err != nil {
		t.Fatalf("GetRepo: %v (requests %v)", err, *seen)
	}
	if repo.Owner != "platform/backend" || repo.Name != "inventory-api" || !repo.Private {
		t.Fatalf("repo = %+v", repo)
	}
	st, err := g.CommitStatus(context.Background(), "platform/backend", "inventory-api", "akili/tsk_1")
	if err != nil || st.State != "none" || st.SHA != "abc123" {
		t.Fatalf("status = %+v %v (requests %v)", st, err, *seen)
	}
	if got := g.GitURL("platform/backend", "inventory-api"); !strings.HasSuffix(got, "/platform/backend/inventory-api.git") {
		t.Fatalf("GitURL = %s", got)
	}
}

func TestGitLabMergeRequestStates(t *testing.T) {
	for state, want := range map[string]PR{
		"opened": {State: "open"},
		"merged": {State: "closed", Merged: true},
		"closed": {State: "closed"},
		"locked": {State: "closed"},
	} {
		got := gitlabMR{State: state}.pr()
		if got.State != want.State || got.Merged != want.Merged {
			t.Errorf("%s: got %s merged=%v", state, got.State, got.Merged)
		}
	}
	if !(gitlabMR{WIP: true}).pr().Draft || !(gitlabMR{Draft: true}).pr().Draft {
		t.Error("draft not read")
	}
}

func TestGitLabCreateMRDraftPrefix(t *testing.T) {
	var body map[string]any
	g, _ := gitlabFake(t, map[string]func(http.ResponseWriter, *http.Request){
		"POST /api/v4/projects/g%2Fr/merge_requests": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&body)
			_, _ = io.WriteString(w, `{"iid":12,"state":"opened","title":"Draft: Fix","draft":true,"source_branch":"akili/t","target_branch":"main"}`)
		},
	})
	pr, err := g.CreatePR(context.Background(), "g", "r", "akili/t", "main", "Fix", "Closes #3", true)
	if err != nil {
		t.Fatal(err)
	}
	if body["title"] != "Draft: Fix" || body["source_branch"] != "akili/t" || body["target_branch"] != "main" || body["description"] != "Closes #3" {
		t.Fatalf("request body = %v", body)
	}
	if pr.Number != 12 || !pr.Draft || pr.State != "open" {
		t.Fatalf("pr = %+v", pr)
	}
}

func TestGitLabCIStates(t *testing.T) {
	cases := map[string]string{
		"success": "success", "created": "pending", "waiting_for_resource": "pending", "preparing": "pending",
		"pending": "pending", "running": "pending", "scheduled": "pending", "failed": "failure", "canceled": "error",
		"skipped": "", "manual": "",
	}
	for in, want := range cases {
		if got := gitlabState(in, false); got != want {
			t.Errorf("%s: got %q, want %q", in, got, want)
		}
	}
	if gitlabState("failed", true) != "success" {
		t.Error("allow_failure job failed the pipeline")
	}

	g, _ := gitlabFake(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET /api/v4/projects/g%2Fr/repository/branches/b": reply(`{"commit":{"id":"s1"}}`),
		"GET /api/v4/projects/g%2Fr/repository/commits/s1/statuses": reply(`[
			{"id":1,"name":"test","status":"failed"},
			{"id":3,"name":"test","status":"success"},
			{"id":2,"name":"deploy","status":"manual"},
			{"id":4,"name":"lint","status":"running"}]`),
	})
	st, err := g.CommitStatus(context.Background(), "g", "r", "b")
	if err != nil {
		t.Fatal(err)
	}
	if st.State != "pending" || len(st.Checks) != 2 {
		t.Fatalf("status = %+v (a retried job must count once, manual not at all)", st)
	}
}

func TestGitLabDiffFallsBackToPagedDiffs(t *testing.T) {
	const mr = "/api/v4/projects/g%2Fr/merge_requests/7"
	g, seen := gitlabFake(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET " + mr + "/diffs": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("page") == "1" {
				w.Header().Set("X-Next-Page", "2")
				_, _ = io.WriteString(w, `[{"old_path":"a.go","new_path":"a.go","diff":"@@ -1 +1 @@\n-x\n+y\n"}]`)
				return
			}
			_, _ = io.WriteString(w, `[{"old_path":"n.txt","new_path":"n.txt","new_file":true,"diff":"@@ -0,0 +1 @@\n+hi"}]`)
		},
	})
	diff, err := g.PRDiff(context.Background(), "g", "r", 7)
	if err != nil {
		t.Fatalf("%v (requests %v)", err, *seen)
	}
	for _, want := range []string{"diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-x\n+y\n", "--- /dev/null\n+++ b/n.txt\n@@ -0,0 +1 @@\n+hi\n"} {
		if !strings.Contains(diff, want) {
			t.Errorf("diff misses %q:\n%s", want, diff)
		}
	}

	g2, _ := gitlabFake(t, map[string]func(http.ResponseWriter, *http.Request){"GET " + mr + "/raw_diffs": reply("diff --git a/x b/x\n")})
	if diff, err := g2.PRDiff(context.Background(), "g", "r", 7); err != nil || diff != "diff --git a/x b/x\n" {
		t.Fatalf("raw_diffs: %q %v", diff, err)
	}
}

func TestGitLabTokenInfoAndAuth(t *testing.T) {
	g, _ := gitlabFake(t, map[string]func(http.ResponseWriter, *http.Request){
		"GET /api/v4/user":                         reply(`{"id":9,"username":"project_42_bot_abc","bot":true}`),
		"GET /api/v4/personal_access_tokens/self":  reply(`{"expires_at":"2027-03-01"}`),
		"GET /api/v4/projects/g%2Fr/members/all/9": reply(`{"access_level":30}`),
	})
	info, err := g.TokenInfo(context.Background())
	if err != nil || info.Kind != "project" || info.ExpiresAt == nil || info.ExpiresAt.Format("2006-01-02") != "2027-03-01" {
		t.Fatalf("info = %+v %v", info, err)
	}
	if lvl, err := g.AccessLevel(context.Background(), "g", "r"); err != nil || lvl != GitLabDeveloper {
		t.Fatalf("access level = %d %v", lvl, err)
	}
	if user, pass, _ := g.GitAuth(context.Background()); user != "oauth2" || pass != "glpat-test" {
		t.Fatalf("git auth = %s/%s", user, pass)
	}
	if who, err := g.Verify(context.Background()); err != nil || who != "project_42_bot_abc" {
		t.Fatalf("verify = %s %v", who, err)
	}
}

func TestGitLabErrorMessages(t *testing.T) {
	if got := errorMessage([]byte(`{"message":{"name":["has already been taken"]}}`)); !strings.Contains(got, "already been taken") {
		t.Errorf("field errors: %q", got)
	}
	if got := errorMessage([]byte(`{"error":"insufficient_scope"}`)); got != "insufficient_scope" {
		t.Errorf("error field: %q", got)
	}
}
