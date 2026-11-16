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

// Command fakemiabi serves an in-memory Miabi API for end-to-end tests (controls: POST /_fake/unhealthy, GET /_fake/state).
package main

import (
	"flag"
	"log"
	"net/http"
	"strings"

	"github.com/goakili/akili/server/internal/miabi/miabitest"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:18681", "listen address")
	key := flag.String("key", "mb_dev", "API key clients must send")
	tags := flag.String("tags", "v1,v2", "initial releases of the app \"api\"; the last is active")
	flag.Parse()
	log.Printf("fake Miabi on %s (app api, releases %s)", *addr, *tags)
	log.Fatal(http.ListenAndServe(*addr, miabitest.New(*key, strings.Split(*tags, ",")...)))
}
