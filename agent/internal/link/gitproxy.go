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
	"crypto/rand"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/goakili/akili/proto"
	"github.com/jkaninda/logger"
)

// gitProxy listens on loopback behind a random per-process path secret; the control plane adds forge credentials.
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
