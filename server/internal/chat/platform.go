// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package chat connects chat platforms (Slack, Telegram, Signal) to agent sessions. Gateways are thin
// clients of the session API: every chat user is linked to an Akili user, and their messages,
// tasks and approvals run with that user's role and are audited as that user.
package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Incoming is a message or a button press from a chat user.
type Incoming struct {
	ChatID   string // the conversation: DM, group or channel
	UserID   string // the platform's user id
	UserName string
	Text     string
	// Action is a button's data (e.g. "ap:<approval id>:approve"); ActionRef acknowledges it.
	Action    string
	ActionRef string
}

// Button is an inline action under a message.
type Button struct {
	Label string
	Data  string
}

// Outgoing is a reply.
type Outgoing struct {
	Text    string
	Buttons []Button
}

// Platform sends to one chat channel.
type Platform interface {
	Send(ctx context.Context, chatID string, msg Outgoing) error
	// Test checks the credentials and returns the bot's identity.
	Test(ctx context.Context) (string, error)
}

// Poller receives messages by polling (no inbound exposure needed).
type Poller interface {
	Poll(ctx context.Context, cursor int64) ([]Incoming, int64, error)
}

// Acker acknowledges a button press (Telegram stops the button's spinner).
type Acker interface {
	Ack(ctx context.Context, ref, text string) error
}

// maxChunk keeps messages under every platform's limit (Telegram: 4096 characters).
const maxChunk = 3500

func chunks(s string) []string {
	r := []rune(s)
	if len(r) <= maxChunk {
		return []string{s}
	}
	var out []string
	for len(r) > 0 {
		n := min(maxChunk, len(r))
		// Prefer to cut at a line break.
		if n < len(r) {
			if i := strings.LastIndex(string(r[:n]), "\n"); i > maxChunk/2 {
				n = len([]rune(string(r[:n])[:i]))
			}
		}
		out = append(out, string(r[:n]))
		r = r[n:]
	}
	return out
}

var httpClient = &http.Client{Timeout: 60 * time.Second}

// postJSON sends a JSON request and decodes a JSON response.
func postJSON(ctx context.Context, url string, headers map[string]string, in, out any) error {
	b, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return do(req, out)
}

func do(req *http.Request, out any) error {
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return fmt.Errorf("%s: %d %s", req.URL.Path[strings.LastIndex(req.URL.Path, "/")+1:], resp.StatusCode, msg)
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// jsonRaw defers decoding of a nested result.
type jsonRaw []byte

func (r *jsonRaw) UnmarshalJSON(b []byte) error { *r = append((*r)[:0], b...); return nil }

func (r jsonRaw) decode(out any) error {
	if len(r) == 0 {
		return nil
	}
	return json.Unmarshal(r, out)
}
