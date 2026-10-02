// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package miabi

import (
	"context"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/goakili/akili/server/internal/miabi/miabitest"
)

func TestClientAgainstFake(t *testing.T) {
	f := miabitest.New("mb_test", "v1", "v2")
	srv := httptest.NewServer(f)
	defer srv.Close()
	c := New(srv.URL, "7", "mb_test")
	c.Poll = 50 * time.Millisecond
	ctx := context.Background()

	if who, err := c.Me(ctx); err != nil || who == "" {
		t.Fatalf("me: %q %v", who, err)
	}
	if _, err := New(srv.URL, "7", "mb_wrong").Me(ctx); err == nil {
		t.Fatal("wrong key accepted")
	}
	app, err := c.Resolve(ctx, "api")
	if err != nil || app.ID != 1 || app.Tag != "v2" {
		t.Fatalf("resolve: %+v %v", app, err)
	}
	if _, err := c.Resolve(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown app: %v", err)
	}

	f.SetUnhealthy("v3", true)
	d, err := c.Deploy(ctx, app.ID, "v3")
	if err != nil {
		t.Fatal(err)
	}
	done, err := c.Wait(ctx, app.ID, d.ID, 5*time.Second)
	if err != nil || done.Status != "succeeded" {
		t.Fatalf("wait: %+v %v", done, err)
	}
	st, _ := c.Status(ctx, app.ID)
	if st.Health != "unhealthy" {
		t.Fatalf("health after bad deploy = %s", st.Health)
	}
	logs, err := c.Logs(ctx, app.ID, 50)
	if err != nil || !strings.Contains(logs, "500") {
		t.Fatalf("logs: %q %v", logs, err)
	}

	rels, _ := c.Releases(ctx, app.ID)
	var v2 int64
	for _, r := range rels {
		if strings.HasSuffix(r.Image, ":v2") {
			if r.Version != 2 {
				t.Fatalf("release v2 numbered %d, want 2", r.Version)
			}
			v2 = r.ID
		}
	}
	rb, err := c.Rollback(ctx, app.ID, v2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Wait(ctx, app.ID, rb.ID, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if f.ActiveTag("api") != "v2" {
		t.Fatalf("active after rollback = %s", f.ActiveTag("api"))
	}
	if st, _ := c.Status(ctx, app.ID); st.Health != "healthy" {
		t.Fatalf("health after rollback = %s", st.Health)
	}
}

func TestWorkspacesAndEvents(t *testing.T) {
	f := miabitest.New("mb_test", "v1", "v2")
	f.DeployDelay = 50 * time.Millisecond
	srv := httptest.NewServer(f)
	defer srv.Close()
	ctx := context.Background()
	c := New(srv.URL, "", "mb_test")
	c.Poll = 20 * time.Millisecond

	ws, err := c.Workspaces(ctx)
	if err != nil || len(ws) != 2 || ws[0].Name != "staging" || ws[1].Name != "prod" {
		t.Fatalf("workspaces: %+v %v", ws, err)
	}
	b, err := c.Binding(ctx)
	if err != nil || b.WorkspaceID != 0 || b.User == "" {
		t.Fatalf("binding: %+v %v", b, err)
	}
	prod := c.In("prod")
	if _, err := prod.ResolveName(ctx, "11"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an app id resolved by name: %v", err)
	}
	if _, err := prod.ResolveName(ctx, "uid-11"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an app uid resolved by name: %v", err)
	}
	app, err := prod.ResolveName(ctx, "API")
	if err != nil || app.ID != 11 {
		t.Fatalf("resolve prod/api: %+v %v", app, err)
	}

	got := make(chan Event, 8)
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() { _ = prod.StreamEvents(sctx, func(e Event) { got <- e }) }()
	time.Sleep(100 * time.Millisecond) // let the stream subscribe
	d, err := prod.Deploy(ctx, app.ID, "v3")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prod.Wait(ctx, app.ID, d.ID, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	timeout := time.After(3 * time.Second)
	for !seen["deploy.succeeded"] {
		select {
		case e := <-got:
			if e.WorkspaceID != 8 || e.ApplicationID != 11 {
				t.Fatalf("event from the wrong place: %+v", e)
			}
			seen[e.Type] = true
		case <-timeout:
			t.Fatalf("no deploy.succeeded on the stream; saw %v", seen)
		}
	}
	recent, err := prod.RecentEvents(ctx, 50)
	if err != nil || len(recent) < 2 || recent[0].ID > recent[len(recent)-1].ID || recent[len(recent)-1].AppName != "api" {
		t.Fatalf("recent: %+v %v", recent, err)
	}
	if staging, _ := c.In("staging").RecentEvents(ctx, 50); len(staging) != 0 {
		t.Fatalf("prod events leaked into staging: %+v", staging)
	}
}

// A Miabi with a private-CA certificate is reachable only once its CA is trusted.
func TestTrustCA(t *testing.T) {
	srv := httptest.NewTLSServer(miabitest.New("mb_test", "v1"))
	defer srv.Close()
	ctx := context.Background()
	if _, err := New(srv.URL, "staging", "mb_test").Me(ctx); err == nil {
		t.Fatal("an untrusted certificate was accepted")
	}
	ca := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}))
	c := New(srv.URL, "staging", "mb_test")
	if err := c.TrustCA(ca); err != nil {
		t.Fatal(err)
	}
	if who, err := c.Me(ctx); err != nil || who == "" {
		t.Fatalf("with the CA: %q %v", who, err)
	}
	if err := New(srv.URL, "", "k").TrustCA("not a certificate"); err == nil {
		t.Fatal("garbage accepted as a CA")
	}
}

// A workspace-bound key may not list workspaces; it can still fetch its own.
func TestBoundKeyWorkspace(t *testing.T) {
	f := miabitest.New("mb_test", "v1")
	srv := httptest.NewServer(f)
	defer srv.Close()
	ctx := context.Background()
	if resp, err := http.Post(srv.URL+"/_fake/bind", "application/json", strings.NewReader(`{"workspace_id":7}`)); err != nil {
		t.Fatal(err)
	} else {
		resp.Body.Close()
	}
	c := New(srv.URL, "", "mb_test")
	if _, err := c.Workspaces(ctx); err == nil {
		t.Fatal("a bound key listed workspaces")
	}
	w, err := c.Workspace(ctx, "7")
	if err != nil || w.Name != "staging" {
		t.Fatalf("own workspace: %+v %v", w, err)
	}
	if _, err := c.Workspace(ctx, "8"); err == nil {
		t.Fatal("a bound key read another workspace")
	}
}
