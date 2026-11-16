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

// Command fakechat serves fake Telegram, Slack and Signal APIs for end-to-end tests.
package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/goakili/akili/server/internal/chat/chattest"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:18760", "listen address")
	tg := flag.String("telegram-token", "123:telegram-test-token", "Telegram bot token")
	slack := flag.String("slack-token", "xoxb-slack-test-token", "Slack bot token")
	signal := flag.String("signal-account", "+15550001111", "Signal bot number")
	flag.Parse()
	log.Printf("fake chat APIs on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, chattest.New(*tg, *slack, *signal))) //nolint:gosec // test tool
}
