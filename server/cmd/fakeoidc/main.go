// Copyright (c) 2026 Jonas Kaninda.
//
// This file is part of Akili.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.
//
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
