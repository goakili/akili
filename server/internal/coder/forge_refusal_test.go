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

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/models"
)

// A forge that refuses writes (403) must be explained as a missing integration write permission, not left to the agent to guess.
func TestForgeWriteRefusalIsExplained(t *testing.T) {
	forge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("service") == "git-receive-pack" {
			http.Error(w, "Write access to repository not granted.", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		_, _ = io.WriteString(w, "001e# service=git-upload-pack\n0000")
	}))
	defer forge.Close()

	s := testService(t)
	if err := s.db.AutoMigrate(&models.ChatSession{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	it := &models.Integration{Base: models.Base{ID: "int_gh", OrganizationID: "org_1"}}
	if err := s.SaveIntegration(ctx, it, IntegrationInput{Name: "github-bot", Kind: models.ForgeGitea, BaseURL: forge.URL, Token: "t"}); err != nil {
		t.Fatal(err)
	}
	prj := &models.Project{Base: models.Base{ID: "prj_1", OrganizationID: "org_1"}, Name: "site", Slug: "site", IntegrationID: it.ID, Forge: models.ForgeGitHub,
		Owner: "goakili", Repo: "website-2", DefaultBranch: "main", Selector: []string{}}
	project := prj.ID
	sess := &models.ChatSession{Base: models.Base{ID: "ses_1", OrganizationID: "org_1"}, AgentID: "ag_1", ProjectID: &project, Branch: "akili/tsk_1", Status: models.SessionOpen}
	for _, v := range []any{prj, sess} {
		if err := s.db.Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}
	proxy := s.GitProxy("ag_1")

	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, proto.GitProxyPath+"ses_1/repo.git/info/refs?service=git-receive-pack", nil))
	body := rec.Body.String()
	if rec.Code != http.StatusForbidden || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("status %d, type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	for _, want := range []string{`GitHub refused to let the "github-bot" integration push to goakili/website-2 (HTTP 403: Write access to repository not granted)`,
		"not Akili's push guard", "Contents and Pull requests set to read and write"} {
		if !strings.Contains(body, want) {
			t.Errorf("message lacks %q:\n%s", want, body)
		}
	}

	// Reads are untouched: a clone still works with read-only credentials.
	rec = httptest.NewRecorder()
	proxy.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, proto.GitProxyPath+"ses_1/repo.git/info/refs?service=git-upload-pack", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "service=git-upload-pack") {
		t.Fatalf("fetch: %d %q", rec.Code, rec.Body.String())
	}
}

func TestForgeWriteRefusalText(t *testing.T) {
	gl := forgeWriteRefusal("GitLab", "", "platform/backend/api", 403, "  You are not allowed\n to push code to this project.  ")
	if !strings.HasPrefix(gl, "GitLab refused to let the integration push to platform/backend/api (HTTP 403: You are not allowed to push code to this project)") ||
		!strings.Contains(gl, "Developer role") {
		t.Fatalf("gitlab: %s", gl)
	}
	if long := forgeWriteRefusal("Gitea", "g", "o/r", 401, strings.Repeat("x", 900)); strings.Count(long, "x") > 200 {
		t.Fatal("the forge's message was not cut")
	}
}
