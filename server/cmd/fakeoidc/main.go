// Copyright (c) 2026 Jonas Kaninda.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
