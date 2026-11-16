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

// Package notify sends outbound notifications to a webhook as {"text": ...}, which most chat incoming-webhooks accept.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jkaninda/logger"
)

// Notifier posts messages to a webhook; a zero URL disables it.
type Notifier struct {
	url       string
	publicURL string
	http      *http.Client
	mail      *Mailer
}

func (n *Notifier) SetMailer(m *Mailer) { n.mail = m }

// Mail returns the mailer; its methods do nothing on nil.
func (n *Notifier) Mail() *Mailer {
	if n == nil {
		return nil
	}
	return n.mail
}

func New(url, publicURL string) *Notifier {
	return &Notifier{url: url, publicURL: publicURL, http: &http.Client{Timeout: 10 * time.Second}}
}

func (n *Notifier) Link(path string) string { return n.publicURL + path }

// Send posts text asynchronously; failures are logged, never propagated.
func (n *Notifier) Send(text string) {
	if n == nil || n.url == "" {
		return
	}
	go func() {
		body, _ := json.Marshal(map[string]string{"text": text})
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.url, bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := n.http.Do(req)
		if err != nil {
			logger.Warn("notification failed", "error", err)
			return
		}
		_ = resp.Body.Close()
		if resp.StatusCode >= 300 {
			logger.Warn("notification rejected", "status", resp.Status)
		}
	}()
}
