// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package miabi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Payloads are shaped like the real Miabi API (numeric release versions, newest deployment first).
const testReleases = `[
 {"id":20,"deployment_id":24,"version":11,"image":"app:3.0.0","active":true,"created_at":"2026-10-01T15:20:49Z"},
 {"id":19,"deployment_id":23,"version":10,"image":"app:2.0.0","active":false,"created_at":"2026-10-01T15:17:03Z"}]`

func stubMiabi(t *testing.T, deployments string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		data := ""
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases"):
			data = testReleases
		case strings.HasSuffix(r.URL.Path, "/deployments"):
			data = deployments
		default:
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"success":true,"data":`+data+`}`)
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, "", "mb_test").In("system")
}

func TestPreviousReleaseAfterFailedDeploy(t *testing.T) {
	c := stubMiabi(t, `[
 {"id":26,"number":14,"status":"failed","image":"app:9.9.9"},
 {"id":24,"number":12,"status":"succeeded","image":"app:3.0.0"},
 {"id":23,"number":11,"status":"succeeded","image":"app:2.0.0"}]`)
	_, err := previousRelease(context.Background(), c, 6)
	var kept *keptRelease
	if !errors.As(err, &kept) || kept.image != "app:3.0.0" || kept.deployment != 14 {
		t.Fatalf("a failed deploy must leave the running release alone, got %v", err)
	}
}

func TestPreviousReleaseAfterBadDeploy(t *testing.T) {
	c := stubMiabi(t, `[
 {"id":24,"number":12,"status":"succeeded","image":"app:3.0.0"},
 {"id":23,"number":11,"status":"succeeded","image":"app:2.0.0"}]`)
	id, err := previousRelease(context.Background(), c, 6)
	if err != nil || id != 19 {
		t.Fatalf("rollback target = %d, %v; want release 19", id, err)
	}
}
