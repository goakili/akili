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

package middlewares

import (
	"net/http"
	"testing"
)

func TestIsWebSocketUpgrade(t *testing.T) {
	req := func(hdr map[string]string) *http.Request {
		r, err := http.NewRequest(http.MethodGet, "https://akili.example/api/v1/agents/agt_1/terminal", nil)
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		return r
	}
	upgrades := []map[string]string{
		{"Upgrade": "websocket", "Connection": "Upgrade"},
		{"Upgrade": "WebSocket", "Connection": "keep-alive, Upgrade"},
		{"Upgrade": "WEBSOCKET", "Connection": "upgrade"},
	}
	for _, hdr := range upgrades {
		if !isWebSocketUpgrade(req(hdr)) {
			t.Fatalf("headers %v not seen as a WebSocket upgrade", hdr)
		}
	}
	plain := []map[string]string{
		{},
		{"Upgrade": "websocket"},
		{"Connection": "Upgrade"},
		{"Upgrade": "h2c", "Connection": "Upgrade"},
	}
	for _, hdr := range plain {
		if isWebSocketUpgrade(req(hdr)) {
			t.Fatalf("headers %v wrongly seen as a WebSocket upgrade", hdr)
		}
	}
}
