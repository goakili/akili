// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package chattest fakes the Telegram Bot API, the Slack Web API and a signal-cli REST server for
// tests, with /_fake controls to inject user messages and read what the bot sent.
package chattest

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Sent is one message the bot sent.
type Sent struct {
	Platform string   `json:"platform"`
	Chat     string   `json:"chat"`
	Text     string   `json:"text"`
	Buttons  []string `json:"buttons,omitempty"` // button data
}

// Fake is the fake server.
type Fake struct {
	TelegramToken string
	SlackToken    string
	SignalAccount string

	mu       sync.Mutex
	nextID   int64
	tgQueue  []map[string]any
	sigQueue []map[string]any
	sent     []Sent
	wake     chan struct{}
}

// New returns a fake.
func New(telegramToken, slackToken, signalAccount string) *Fake {
	return &Fake{TelegramToken: telegramToken, SlackToken: slackToken, SignalAccount: signalAccount, wake: make(chan struct{}, 1)}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (f *Fake) record(s Sent) {
	f.mu.Lock()
	f.sent = append(f.sent, s)
	f.mu.Unlock()
}

// ServeHTTP routes the three APIs and the controls.
func (f *Fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	switch {
	case strings.HasPrefix(p, "/_fake/"):
		f.control(w, r)
	case strings.HasPrefix(p, "/bot"):
		f.telegram(w, r)
	case strings.HasPrefix(p, "/api/"):
		f.slack(w, r)
	case strings.HasPrefix(p, "/v1/") || strings.HasPrefix(p, "/v2/"):
		f.signal(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (f *Fake) telegram(w http.ResponseWriter, r *http.Request) {
	token, method, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/bot"), "/")
	if token != f.TelegramToken {
		writeJSON(w, 401, map[string]any{"ok": false, "description": "Unauthorized"})
		return
	}
	var in map[string]any
	_ = json.NewDecoder(r.Body).Decode(&in)
	switch method {
	case "getMe":
		writeJSON(w, 200, map[string]any{"ok": true, "result": map[string]any{"id": 1, "is_bot": true, "username": "akili_test_bot"}})
	case "getUpdates":
		offset, _ := in["offset"].(float64)
		deadline := time.Now().Add(2 * time.Second)
		for {
			f.mu.Lock()
			var out []map[string]any
			for _, u := range f.tgQueue {
				if u["update_id"].(int64) >= int64(offset) {
					out = append(out, u)
				}
			}
			f.mu.Unlock()
			if len(out) > 0 || time.Now().After(deadline) {
				writeJSON(w, 200, map[string]any{"ok": true, "result": out})
				return
			}
			select {
			case <-f.wake:
			case <-time.After(200 * time.Millisecond):
			case <-r.Context().Done():
				return
			}
		}
	case "sendMessage":
		s := Sent{Platform: "telegram", Chat: jsonString(in["chat_id"]), Text: jsonString(in["text"])}
		if rm, ok := in["reply_markup"].(map[string]any); ok {
			for _, row := range rm["inline_keyboard"].([]any) {
				for _, b := range row.([]any) {
					s.Buttons = append(s.Buttons, b.(map[string]any)["callback_data"].(string))
				}
			}
		}
		f.record(s)
		writeJSON(w, 200, map[string]any{"ok": true, "result": map[string]any{"message_id": 1}})
	case "answerCallbackQuery":
		writeJSON(w, 200, map[string]any{"ok": true, "result": true})
	default:
		writeJSON(w, 404, map[string]any{"ok": false, "description": "Not Found"})
	}
}

func jsonString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatInt(int64(x), 10)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func jsonNum(x float64) string { return strconv.FormatInt(int64(x), 10) }

func (f *Fake) slack(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+f.SlackToken {
		writeJSON(w, 200, map[string]any{"ok": false, "error": "invalid_auth"})
		return
	}
	var in map[string]any
	_ = json.NewDecoder(r.Body).Decode(&in)
	switch strings.TrimPrefix(r.URL.Path, "/api/") {
	case "auth.test":
		writeJSON(w, 200, map[string]any{"ok": true, "user": "akili", "team": "test-team"})
	case "chat.postMessage":
		s := Sent{Platform: "slack", Chat: jsonString(in["channel"]), Text: jsonString(in["text"])}
		if blocks, ok := in["blocks"].([]any); ok {
			for _, b := range blocks {
				if els, ok := b.(map[string]any)["elements"].([]any); ok {
					for _, e := range els {
						s.Buttons = append(s.Buttons, e.(map[string]any)["value"].(string))
					}
				}
			}
		}
		f.record(s)
		writeJSON(w, 200, map[string]any{"ok": true})
	default:
		writeJSON(w, 200, map[string]any{"ok": false, "error": "unknown_method"})
	}
}

func (f *Fake) signal(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/v1/about":
		writeJSON(w, 200, map[string]any{"mode": "json-rpc", "versions": []string{"v1", "v2"}})
	case strings.HasPrefix(r.URL.Path, "/v1/receive/"):
		f.mu.Lock()
		out := f.sigQueue
		f.sigQueue = nil
		f.mu.Unlock()
		if out == nil {
			out = []map[string]any{}
			time.Sleep(300 * time.Millisecond)
		}
		writeJSON(w, 200, out)
	case r.URL.Path == "/v2/send":
		var in struct {
			Message    string   `json:"message"`
			Number     string   `json:"number"`
			Recipients []string `json:"recipients"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.Number != f.SignalAccount {
			writeJSON(w, 400, map[string]string{"error": "unknown account"})
			return
		}
		for _, rc := range in.Recipients {
			f.record(Sent{Platform: "signal", Chat: rc, Text: in.Message})
		}
		writeJSON(w, 201, map[string]any{"timestamp": time.Now().UnixMilli()})
	default:
		http.NotFound(w, r)
	}
}

// Inject is a message (or button press) from a chat user.
type Inject struct {
	Chat     string `json:"chat"`
	User     string `json:"user"`
	Username string `json:"username"`
	Text     string `json:"text"`
	Callback string `json:"callback"` // telegram: button data instead of text
}

func (f *Fake) control(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/_fake/telegram":
		var in Inject
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.mu.Lock()
		f.nextID++
		from := map[string]any{"id": json.Number(in.User), "username": in.Username, "first_name": in.Username}
		u := map[string]any{"update_id": f.nextID}
		if in.Callback != "" {
			u["callback_query"] = map[string]any{"id": "cb" + jsonNum(float64(f.nextID)), "from": from, "data": in.Callback,
				"message": map[string]any{"chat": map[string]any{"id": json.Number(in.Chat)}}}
		} else {
			u["message"] = map[string]any{"message_id": f.nextID, "from": from, "chat": map[string]any{"id": json.Number(in.Chat)}, "text": in.Text}
		}
		f.tgQueue = append(f.tgQueue, u)
		f.mu.Unlock()
		select {
		case f.wake <- struct{}{}:
		default:
		}
		writeJSON(w, 200, u)
	case "/_fake/signal":
		var in Inject
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.mu.Lock()
		f.sigQueue = append(f.sigQueue, map[string]any{"envelope": map[string]any{"source": in.Chat, "sourceNumber": in.Chat, "sourceUuid": in.User,
			"sourceName": in.Username, "dataMessage": map[string]any{"message": in.Text}}})
		f.mu.Unlock()
		writeJSON(w, 200, in)
	case "/_fake/sent":
		f.mu.Lock()
		out := append([]Sent{}, f.sent...)
		f.mu.Unlock()
		writeJSON(w, 200, out)
	default:
		http.NotFound(w, r)
	}
}
