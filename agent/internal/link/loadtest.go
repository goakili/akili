// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package link

import (
	"context"

	"github.com/goakili/akili/proto"
	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"
	"github.com/jkaninda/wstunnel"
)

// DialOnce opens one tunnel with a freshly signed handshake, without Run's reconnect loop, and
// returns the HTTP status of a refused handshake (0 when there was no response). It exists for
// akili-loadtest, which drives many agents from one process and times each connect itself.
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

// Serve answers the control and session streams of one tunnel until it closes, exactly as Run does.
func (a *Agent) Serve(ctx context.Context, sess *yamux.Session) error {
	return a.serve(ctx, sess)
}
