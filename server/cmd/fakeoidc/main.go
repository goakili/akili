// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command fakeoidc serves a fake OpenID Connect provider for end-to-end tests.
package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/goakili/akili/server/internal/auth/oidctest"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:18690", "listen address")
	issuer := flag.String("issuer", "http://127.0.0.1:18690", "issuer URL (must match how clients reach it)")
	id := flag.String("client-id", "akili", "client id")
	secret := flag.String("client-secret", "akili-sso-secret", "client secret")
	flag.Parse()
	p := oidctest.New(*id, *secret)
	p.Issuer = *issuer
	log.Printf("fake OIDC provider %s on %s", *issuer, *addr)
	log.Fatal(http.ListenAndServe(*addr, p)) //nolint:gosec // test tool
}
