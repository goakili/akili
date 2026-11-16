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
