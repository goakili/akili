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
