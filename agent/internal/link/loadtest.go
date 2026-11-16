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
