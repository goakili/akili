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

package link

import (
	"context"

	"github.com/goakili/akili/proto"
	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"
	"github.com/jkaninda/wstunnel"
)

// DialOnce exists for akili-loadtest, which times each connect itself; it returns a refused handshake's status (0 if none).
func (a *Agent) DialOnce(ctx context.Context, dialer *websocket.Dialer) (*yamux.Session, int, error) {
	ws, resp, err := dialer.DialContext(ctx, wstunnel.URL(a.st.URL, proto.ConnectPath), a.handshake())
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		return nil, status, err
	}
	sess, err := wstunnel.Server(ws)
	if err != nil {
		_ = ws.Close()
		return nil, 0, err
	}
	return sess, 0, nil
}

func (a *Agent) Serve(ctx context.Context, sess *yamux.Session) error {
	return a.serve(ctx, sess)
}
