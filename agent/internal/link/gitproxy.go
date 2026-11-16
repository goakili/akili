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
