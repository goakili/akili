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
