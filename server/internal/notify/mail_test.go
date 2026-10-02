// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/goakili/akili/server/internal/models"
)

func TestRenderEscapesAndKeepsLinksAbsolute(t *testing.T) {
	m := &Mailer{publicURL: "https://akili.example.com/"}
	text, html, err := m.render(Message{
		Heading: "Your task succeeded",
		Rows:    [][2]string{{"Task", "<script>alert(1)</script>\nBcc: evil@example.com"}},
		Link:    "/tasks/tsk_1", Action: "Open the task",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "<script>") || !strings.Contains(html, "&lt;script&gt;") {
		t.Fatalf("user text not escaped in HTML:\n%s", html)
	}
	if strings.Contains(text, "\nBcc:") {
		t.Fatalf("a newline in user text survived:\n%s", text)
	}
	if !strings.Contains(html, `href="https://akili.example.com/tasks/tsk_1"`) || !strings.Contains(text, "https://akili.example.com/tasks/tsk_1") {
		t.Fatalf("link is not absolute:\n%s\n%s", text, html)
	}
}

func TestOneLine(t *testing.T) {
	if got := oneLine("Approval needed:\r\nBcc: x@y", 150); strings.ContainsAny(got, "\r\n") {
		t.Fatalf("%q", got)
	}
	if got := oneLine(strings.Repeat("é", 300), 10); len([]rune(got)) != 10 {
		t.Fatalf("not truncated by runes: %q", got)
	}
}

// TestDryRunAgainstPosta drives the real posta-go client against a fake Posta.
func TestDryRunAgainstPosta(t *testing.T) {
	var got struct {
		path, query, auth string
		body              map[string]any
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path, got.query, got.auth = r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got.body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"valid":true}}`))
	}))
	defer srv.Close()
	m := &Mailer{decrypt: func(s string) (string, error) { return s, nil }, clients: map[string]cachedClient{}}
	it := &models.Integration{Base: models.Base{ID: "int_1"}, Kind: models.KindPosta, BaseURL: srv.URL, TokenEnc: "psk_test", Sender: "Akili <akili@example.com>"}
	who, err := m.Test(context.Background(), it)
	if err != nil {
		t.Fatal(err)
	}
	if got.path != "/api/v1/emails/send" || got.query != "dry_run=true" || got.auth != "Bearer psk_test" {
		t.Fatalf("unexpected request: %+v", got)
	}
	if got.body["from"] != "Akili <akili@example.com>" || !strings.Contains(who, "akili@example.com") {
		t.Fatalf("sender: %v %q", got.body["from"], who)
	}
}

func TestPostaErrorsSurface(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"success":false,"error":{"message":"invalid API key"}}`))
	}))
	defer srv.Close()
	m := &Mailer{decrypt: func(s string) (string, error) { return s, nil }, clients: map[string]cachedClient{}}
	it := &models.Integration{Base: models.Base{ID: "int_2"}, BaseURL: srv.URL, TokenEnc: "bad", Sender: "akili@example.com"}
	if _, err := m.Test(context.Background(), it); err == nil || !strings.Contains(err.Error(), "invalid API key") {
		t.Fatalf("error not surfaced: %v", err)
	}
}
