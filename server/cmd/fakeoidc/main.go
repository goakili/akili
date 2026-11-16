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
