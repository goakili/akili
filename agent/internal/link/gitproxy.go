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
	"crypto/rand"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/goakili/akili/proto"
	"github.com/jkaninda/logger"
)

// gitProxy lets the local git client reach a project repository through the control plane. It
// listens on loopback only, behind a random per-process path secret, and forwards requests over the
// current tunnel; the control plane adds the forge credentials and enforces what may be pushed.
type gitProxy struct {
	a      *Agent
	secret string
	addr   string
	client *http.Client
}

func (a *Agent) startGitProxy() error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	gp := &gitProxy{a: a, secret: strings.ToLower(rand.Text()), addr: ln.Addr().String()}
	gp.client = &http.Client{Transport: &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			sess := a.tunnel()
			if sess == nil {
				return nil, errors.New("not connected to the control plane")
			}
			return sess.Open()
		},
		DisableKeepAlives:  true,
		DisableCompression: true,
	}}
	a.git = gp
	go func() {
		if err := http.Serve(ln, gp); err != nil {
			logger.Error("git proxy stopped", "error", err)
		}
	}()
	return nil
}

// Remote is the git URL for a session.
func (g *gitProxy) Remote(sessionID string) string {
	return "http://" + g.addr + "/" + g.secret + "/" + sessionID + "/repo.git"
}

func (g *gitProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rest, ok := strings.CutPrefix(r.URL.Path, "/"+g.secret+"/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	target := "http://control-plane" + proto.GitProxyPath + rest
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	for _, h := range []string{"Content-Type", "Accept", "Git-Protocol", "Content-Encoding"} {
		if v := r.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	req.ContentLength = r.ContentLength
	resp, err := g.client.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for _, h := range []string{"Content-Type", "Cache-Control", "Content-Encoding"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}
