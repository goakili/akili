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
