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
