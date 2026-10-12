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
