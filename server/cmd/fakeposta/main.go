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
