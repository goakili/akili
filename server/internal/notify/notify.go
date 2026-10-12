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

// Package notify sends outbound notifications (approval requests, task results) to a webhook. The
// payload {"text": ...} is accepted by Slack, Mattermost and most chat incoming-webhooks.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jkaninda/logger"
)

// Notifier posts messages to a webhook. A zero URL disables it.
type Notifier struct {
	url       string
	publicURL string
	http      *http.Client
	mail      *Mailer
}

// SetMailer adds email notifications.
func (n *Notifier) SetMailer(m *Mailer) { n.mail = m }

// Mail returns the mailer; its methods do nothing on nil.
func (n *Notifier) Mail() *Mailer {
	if n == nil {
		return nil
	}
	return n.mail
}

// New returns a notifier.
func New(url, publicURL string) *Notifier {
	return &Notifier{url: url, publicURL: publicURL, http: &http.Client{Timeout: 10 * time.Second}}
}

// Link builds an absolute UI link.
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
