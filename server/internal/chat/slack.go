// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package chat

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Slack is a Slack app: events arrive on the Events API and interactivity webhooks (signed with the
// app's signing secret); replies go through chat.postMessage with the bot token.
type Slack struct {
	base   string // https://slack.com/api
	token  string
	secret string
}

// NewSlack returns a Slack client. base is empty for the public API.
func NewSlack(base, token, signingSecret string) *Slack {
	if base == "" {
		base = "https://slack.com/api"
	}
	return &Slack{base: strings.TrimRight(base, "/"), token: token, secret: signingSecret}
}

type slackResp struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
	User  string `json:"user"`
	Team  string `json:"team"`
}

func (s *Slack) call(ctx context.Context, method string, in any) (*slackResp, error) {
	var r slackResp
	if err := postJSON(ctx, s.base+"/"+method, map[string]string{"Authorization": "Bearer " + s.token}, in, &r); err != nil {
		return nil, err
	}
	if !r.OK {
		return nil, errors.New("slack " + method + ": " + r.Error)
	}
	return &r, nil
}

// Test implements Platform.
func (s *Slack) Test(ctx context.Context) (string, error) {
	r, err := s.call(ctx, "auth.test", map[string]any{})
	if err != nil {
		return "", err
	}
	return r.User + " in " + r.Team, nil
}

// Send implements Platform.
func (s *Slack) Send(ctx context.Context, chatID string, msg Outgoing) error {
	parts := chunks(msg.Text)
	for i, p := range parts {
		body := map[string]any{"channel": chatID, "text": p, "unfurl_links": false}
		if i == len(parts)-1 && len(msg.Buttons) > 0 {
			elems := make([]map[string]any, len(msg.Buttons))
			for j, b := range msg.Buttons {
				elems[j] = map[string]any{"type": "button", "action_id": b.Data, "value": b.Data,
					"text": map[string]string{"type": "plain_text", "text": b.Label}}
			}
			body["blocks"] = []map[string]any{
				{"type": "section", "text": map[string]string{"type": "plain_text", "text": p}},
				{"type": "actions", "elements": elems},
			}
		}
		if _, err := s.call(ctx, "chat.postMessage", body); err != nil {
			return err
		}
	}
	return nil
}

// ErrBadSignature means the request did not come from Slack (or is a replay).
var ErrBadSignature = errors.New("invalid Slack signature")

// Verify checks Slack's v0 signature over the raw body and refuses requests older than five minutes.
func (s *Slack) Verify(h http.Header, body []byte, now time.Time) error {
	ts := h.Get("X-Slack-Request-Timestamp")
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || math.Abs(now.Sub(time.Unix(sec, 0)).Seconds()) > 300 {
		return ErrBadSignature
	}
	m := hmac.New(sha256.New, []byte(s.secret))
	m.Write([]byte("v0:" + ts + ":"))
	m.Write(body)
	want := "v0=" + hex.EncodeToString(m.Sum(nil))
	if s.secret == "" || !hmac.Equal([]byte(want), []byte(h.Get("X-Slack-Signature"))) {
		return ErrBadSignature
	}
	return nil
}

var slackMention = regexp.MustCompile(`<@[A-Z0-9]+>\s*`)

// SlackEvent is the result of parsing an Events API request.
type SlackEvent struct {
	Challenge string // url_verification: echo it back
	EventID   string // for de-duplicating Slack's retries
	Message   *Incoming
}

// ParseEvent reads an Events API body. Bot messages, edits and other subtypes are ignored.
func ParseEvent(body []byte) (*SlackEvent, error) {
	var env struct {
		Type      string `json:"type"`
		Challenge string `json:"challenge"`
		EventID   string `json:"event_id"`
		Event     struct {
			Type        string `json:"type"`
			Subtype     string `json:"subtype"`
			BotID       string `json:"bot_id"`
			User        string `json:"user"`
			Text        string `json:"text"`
			Channel     string `json:"channel"`
			ChannelType string `json:"channel_type"`
		} `json:"event"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, err
	}
	out := &SlackEvent{EventID: env.EventID}
	switch env.Type {
	case "url_verification":
		out.Challenge = env.Challenge
		return out, nil
	case "event_callback":
	default:
		return out, nil
	}
	e := env.Event
	if e.BotID != "" || e.Subtype != "" || e.User == "" {
		return out, nil
	}
	// In channels the bot answers mentions; in DMs, every message.
	if e.Type == "app_mention" || (e.Type == "message" && e.ChannelType == "im") {
		out.Message = &Incoming{ChatID: e.Channel, UserID: e.User, UserName: e.User, Text: strings.TrimSpace(slackMention.ReplaceAllString(e.Text, ""))}
	}
	return out, nil
}

// ParseAction reads an interactivity request (a button press).
func ParseAction(body []byte) (*Incoming, error) {
	form, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, err
	}
	var p struct {
		Type string `json:"type"`
		User struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"user"`
		Channel struct {
			ID string `json:"id"`
		} `json:"channel"`
		Actions []struct {
			Value string `json:"value"`
		} `json:"actions"`
	}
	if err := json.Unmarshal([]byte(form.Get("payload")), &p); err != nil {
		return nil, err
	}
	if p.Type != "block_actions" || len(p.Actions) == 0 || p.User.ID == "" {
		return nil, errors.New("unsupported interaction")
	}
	return &Incoming{ChatID: p.Channel.ID, UserID: p.User.ID, UserName: p.User.Username, Action: p.Actions[0].Value}, nil
}
