// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

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
