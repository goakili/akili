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
