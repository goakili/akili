// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package coder

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/goakili/akili/server/internal/audit"
	"github.com/goakili/akili/server/internal/crypto"
	"github.com/goakili/akili/server/internal/gitid"
	"github.com/goakili/akili/server/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func testService(t *testing.T) *Service {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Integration{}, &models.Project{}); err != nil {
		t.Fatal(err)
	}
	kek, _ := crypto.NewLocalKEK("test-passphrase-0123456789")
	ring, err := crypto.OpenKeyring(context.Background(), kek, crypto.NewMemoryStore(), "")
	if err != nil {
		t.Fatal(err)
	}
	return New(db, crypto.NewBox(ring), audit.New(db), gitid.Config{})
}

func TestValidOwner(t *testing.T) {
	for _, c := range []struct {
		kind, owner string
		ok          bool
	}{
		{models.ForgeGitLab, "platform/backend/team", true},
		{models.ForgeGitLab, "solo", true},
		{models.ForgeGitLab, "a//b", false},
		{models.ForgeGitLab, "../x", false},
		{models.ForgeGitLab, "a/./b", false},
		{models.ForgeGitLab, "", false},
		{models.ForgeGitLab, strings.Repeat("g/", 20) + "g", false},
		{models.ForgeGitLab, strings.TrimSuffix(strings.Repeat("g/", 20), "/"), true},
		{models.ForgeGitHub, "a/b/c", false},
		{models.ForgeGitea, "a/b", false},
		{models.ForgeGitHub, "octo-org", true},
	} {
		if got := validOwner(c.kind, c.owner); got != c.ok {
			t.Errorf("validOwner(%s, %q) = %v, want %v", c.kind, c.owner, got, c.ok)
		}
	}
}

func TestSaveGitLabIntegration(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	it := &models.Integration{Base: models.Base{ID: "int_gl", OrganizationID: "org_1"}}
	if err := s.SaveIntegration(ctx, it, IntegrationInput{Name: "gl", Kind: models.ForgeGitLab, Token: "glpat-xxxxxxxxxxxxxxxxxxxx"}); err != nil {
		t.Fatal(err)
	}
	if it.BaseURL != "https://gitlab.com" || it.AuthType != models.AuthToken {
		t.Fatalf("defaults: %+v", it)
	}
	it2 := &models.Integration{Base: models.Base{ID: "int_gl2", OrganizationID: "org_1"}}
	if err := s.SaveIntegration(ctx, it2, IntegrationInput{Name: "gl2", Kind: models.ForgeGitLab, BaseURL: "https://git.corp/api/v4/", Token: "t"}); err != nil || it2.BaseURL != "https://git.corp" {
		t.Fatalf("api suffix kept: %q %v", it2.BaseURL, err)
	}

	s.Production = true
	in := IntegrationInput{Name: "gl3", Kind: models.ForgeGitLab, BaseURL: "http://gitlab.corp", Token: "t"}
	if err := s.SaveIntegration(ctx, &models.Integration{Base: models.Base{ID: "int_gl3", OrganizationID: "org_1"}}, in); err == nil {
		t.Fatal("plain http accepted in production")
	}
	in.BaseURL = "http://localhost:8929"
	if err := s.SaveIntegration(ctx, &models.Integration{Base: models.Base{ID: "int_gl4", OrganizationID: "org_1"}}, in); err != nil {
		t.Fatalf("localhost refused: %v", err)
	}
	s.Production = false
	in.BaseURL = "http://gitlab.corp"
	if err := s.SaveIntegration(ctx, &models.Integration{Base: models.Base{ID: "int_gl5", OrganizationID: "org_1"}}, in); err != nil {
		t.Fatalf("http refused outside production: %v", err)
	}
}

const glSecret = "webhook-secret-0123"

func gitlabWebhookSetup(t *testing.T) *Service {
	t.Helper()
	s := testService(t)
	ctx := context.Background()
	it := &models.Integration{Base: models.Base{ID: "int_gl", OrganizationID: "org_1"}}
	if err := s.SaveIntegration(ctx, it, IntegrationInput{Name: "gl", Kind: models.ForgeGitLab, Token: "t", WebhookSecret: glSecret}); err != nil {
		t.Fatal(err)
	}
	p := &models.Project{Base: models.Base{ID: "prj_1", OrganizationID: "org_1"}, Name: "inv", Slug: "inv", IntegrationID: it.ID, Forge: models.ForgeGitLab,
		Owner: "Platform/Backend", Repo: "inventory-api", DefaultBranch: "main", TriggerLabel: "akili", Selector: []string{}}
	if err := s.db.Create(p).Error; err != nil {
		t.Fatal(err)
	}
	return s
}

func glHeaders(token, event string) http.Header {
	h := http.Header{}
	if token != "" {
		h.Set("X-Gitlab-Token", token)
	}
	h.Set("X-Gitlab-Event", event)
	return h
}

func glIssue(action, state, labels, changes string) []byte {
	return []byte(`{"object_kind":"issue","object_attributes":{"iid":5,"title":"Fix stock","description":"Counts are off","url":"https://gitlab.com/platform/backend/inventory-api/-/issues/5",` +
		`"state":"` + state + `","action":"` + action + `"},"labels":[` + labels + `],"changes":{` + changes + `},"project":{"path_with_namespace":"platform/backend/inventory-api"}}`)
}

func TestGitLabIssueWebhook(t *testing.T) {
	s := gitlabWebhookSetup(t)
	ctx := context.Background()
	label := `{"title":"Akili"}`
	added := `"labels":{"previous":[{"title":"bug"}],"current":[{"title":"bug"},{"title":"akili"}]}`
	kept := `"labels":{"previous":[{"title":"akili"}],"current":[{"title":"akili"},{"title":"bug"}]}`

	for name, body := range map[string][]byte{
		"open with label":     glIssue("open", "opened", label, ""),
		"label added on edit": glIssue("update", "opened", label, added),
	} {
		trig, err := s.ParseIssueWebhook(ctx, "int_gl", glHeaders(glSecret, "Issue Hook"), body)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if trig.Number != 5 || trig.Project.ID != "prj_1" || trig.Ref != "issue:Platform/Backend/inventory-api#5" || trig.Body != "Counts are off" {
			t.Fatalf("%s: trigger = %+v", name, trig)
		}
	}

	for name, c := range map[string]struct {
		h    http.Header
		body []byte
		want error
	}{
		"wrong token":       {glHeaders("nope", "Issue Hook"), glIssue("open", "opened", label, ""), ErrBadSignature},
		"missing token":     {glHeaders("", "Issue Hook"), glIssue("open", "opened", label, ""), ErrBadSignature},
		"other event":       {glHeaders(glSecret, "Push Hook"), glIssue("open", "opened", label, ""), ErrIgnored},
		"label already set": {glHeaders(glSecret, "Issue Hook"), glIssue("update", "opened", label, kept), ErrIgnored},
		"no labels change":  {glHeaders(glSecret, "Issue Hook"), glIssue("update", "opened", label, ""), ErrIgnored},
		"closed issue":      {glHeaders(glSecret, "Issue Hook"), glIssue("close", "closed", label, ""), ErrIgnored},
		"no label":          {glHeaders(glSecret, "Issue Hook"), glIssue("open", "opened", "", ""), ErrIgnored},
	} {
		if _, err := s.ParseIssueWebhook(ctx, "int_gl", c.h, c.body); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", name, err, c.want)
		}
	}
}

func TestValidTokenComparesWholeValue(t *testing.T) {
	if !validToken("abc", "abc") || validToken("abc", "abd") || validToken("abc", "ab") || validToken("abc", "") {
		t.Fatal("token comparison is wrong")
	}
}

func TestTokenExpiryWarnsOnce(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	now := time.Now()
	soon, later := now.Add(5*24*time.Hour), now.Add(60*24*time.Hour)
	for id, exp := range map[string]*time.Time{"int_soon": &soon, "int_later": &later, "int_none": nil} {
		it := models.Integration{Base: models.Base{ID: id, OrganizationID: "org_1"}, Name: id, Kind: models.ForgeGitLab, BaseURL: "http://127.0.0.1:1",
			AuthType: models.AuthToken, TokenExpiresAt: exp}
		if err := s.db.Create(&it).Error; err != nil {
			t.Fatal(err)
		}
	}
	var warned []string
	warn := func(_ context.Context, it *models.Integration) { warned = append(warned, it.ID) }
	s.checkTokenExpiry(ctx, now, warn)
	s.checkTokenExpiry(ctx, now, warn)
	if len(warned) != 1 || warned[0] != "int_soon" {
		t.Fatalf("warned = %v, want [int_soon] once", warned)
	}
}
