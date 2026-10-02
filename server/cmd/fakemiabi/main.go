// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command fakemiabi serves an in-memory Miabi API for development and end-to-end tests. It is not
// part of a deployment. Controls: POST /_fake/unhealthy {"tag","unhealthy"}, GET /_fake/state.
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
