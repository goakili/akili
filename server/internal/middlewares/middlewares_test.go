// SPDX-FileCopyrightText: 2026 Jonas Kaninda
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
