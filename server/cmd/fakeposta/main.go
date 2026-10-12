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
