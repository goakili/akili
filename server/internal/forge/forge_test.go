// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package forge

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestGitHubAppMintsAndCachesInstallationTokens(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	var mints atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/app/installations/42/access_tokens":
			raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			tok, err := jwt.Parse(raw, func(*jwt.Token) (any, error) { return &key.PublicKey, nil }, jwt.WithValidMethods([]string{"RS256"}))
			if err != nil || !tok.Valid {
				http.Error(w, "bad app jwt", http.StatusUnauthorized)
				return
			}
			if iss, _ := tok.Claims.GetIssuer(); iss != "7" {
				http.Error(w, "wrong issuer", http.StatusUnauthorized)
				return
			}
			mints.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "inst-token", "expires_at": time.Now().Add(time.Hour)})
		case r.URL.Path == "/repos/o/r" && r.Header.Get("Authorization") == "Bearer inst-token":
			_, _ = w.Write([]byte(`{"name":"r","default_branch":"main","owner":{"login":"o"}}`))
		default:
			http.Error(w, "unexpected "+r.URL.Path+" "+r.Header.Get("Authorization"), http.StatusTeapot)
		}
	}))
	defer srv.Close()

	g, err := NewGitHubApp(srv.URL, "https://github.com", 7, 42, pemKey)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		repo, err := g.GetRepo(context.Background(), "o", "r")
		if err != nil || repo.DefaultBranch != "main" {
			t.Fatalf("GetRepo: %+v %v", repo, err)
		}
	}
	if mints.Load() != 1 {
		t.Fatalf("installation token minted %d times, want 1 (cached)", mints.Load())
	}
	if user, pass, _ := g.GitAuth(context.Background()); user != "x-access-token" || pass != "inst-token" {
		t.Fatalf("git auth = %s/%s", user, pass)
	}
}

func TestGitHubStatusMergesChecks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/status"):
			_, _ = w.Write([]byte(`{"sha":"abc","statuses":[{"context":"lint","state":"success"}]}`))
		case strings.HasSuffix(r.URL.Path, "/check-runs"):
			_, _ = w.Write([]byte(`{"check_runs":[{"name":"test","status":"completed","conclusion":"failure"},{"name":"build","status":"in_progress"}]}`))
		}
	}))
	defer srv.Close()
	st, err := NewGitHubToken(srv.URL, "", "t").CommitStatus(context.Background(), "o", "r", "akili/x")
	if err != nil {
		t.Fatal(err)
	}
	if st.State != "failure" || len(st.Checks) != 3 {
		t.Fatalf("status = %+v", st)
	}
}

func TestCombine(t *testing.T) {
	cases := map[string][]Check{
		"none":    nil,
		"success": {{State: "success"}},
		"pending": {{State: "success"}, {State: "pending"}},
		"failure": {{State: "pending"}, {State: "error"}},
	}
	for want, checks := range cases {
		if got := combine(checks); got != want {
			t.Errorf("combine(%v) = %s, want %s", checks, got, want)
		}
	}
}
