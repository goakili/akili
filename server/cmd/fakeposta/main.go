// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command fakeposta serves a fake Posta API for end-to-end tests.
package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/goakili/akili/server/internal/notify/postatest"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:18770", "listen address")
	key := flag.String("key", "psk_e2e_test_key", "accepted API key")
	flag.Parse()
	log.Printf("fake Posta API on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, postatest.New(*key))) //nolint:gosec // test tool
}
