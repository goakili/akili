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
