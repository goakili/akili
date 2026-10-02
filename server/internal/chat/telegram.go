// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package chat

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

// Telegram is a Telegram bot, polled with getUpdates.
type Telegram struct {
	base  string // https://api.telegram.org
	token string
}

// NewTelegram returns a bot client. base is empty for the public Bot API.
func NewTelegram(base, token string) *Telegram {
	if base == "" {
		base = "https://api.telegram.org"
	}
	return &Telegram{base: strings.TrimRight(base, "/"), token: token}
}

func (t *Telegram) url(method string) string { return t.base + "/bot" + t.token + "/" + method }

type tgResp[T any] struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
	Result      T      `json:"result"`
}

func (t *Telegram) call(ctx context.Context, method string, in any, out any) error {
	var r tgResp[jsonRaw]
	if err := postJSON(ctx, t.url(method), nil, in, &r); err != nil {
		return redactToken(err, t.token)
	}
	if !r.OK {
		return errors.New("telegram " + method + ": " + r.Description)
	}
	if out != nil {
		return r.Result.decode(out)
	}
	return nil
}

// redactToken keeps the bot token (it is in the URL path) out of errors and logs.
func redactToken(err error, token string) error {
	if err == nil || token == "" {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), token, "<token>"))
}

// Test implements Platform.
func (t *Telegram) Test(ctx context.Context) (string, error) {
	var me struct {
		Username string `json:"username"`
	}
	if err := t.call(ctx, "getMe", map[string]any{}, &me); err != nil {
		return "", err
	}
	return "@" + me.Username, nil
}

type tgUser struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	IsBot     bool   `json:"is_bot"`
}

func (u tgUser) name() string {
	if u.Username != "" {
		return "@" + u.Username
	}
	return u.FirstName
}

type tgUpdate struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		From tgUser `json:"from"`
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
		Text string `json:"text"`
	} `json:"message"`
	Callback *struct {
		ID      string `json:"id"`
		From    tgUser `json:"from"`
		Data    string `json:"data"`
		Message *struct {
			Chat struct {
				ID int64 `json:"id"`
			} `json:"chat"`
		} `json:"message"`
	} `json:"callback_query"`
}

// Poll implements Poller with long polling. cursor is the last update id seen.
func (t *Telegram) Poll(ctx context.Context, cursor int64) ([]Incoming, int64, error) {
	var ups []tgUpdate
	req := map[string]any{"timeout": 25, "allowed_updates": []string{"message", "callback_query"}}
	if cursor > 0 {
		req["offset"] = cursor + 1
	}
	if err := t.call(ctx, "getUpdates", req, &ups); err != nil {
		return nil, cursor, err
	}
	var out []Incoming
	for _, u := range ups {
		cursor = max(cursor, u.UpdateID)
		switch {
		case u.Message != nil && u.Message.Text != "" && !u.Message.From.IsBot:
			out = append(out, Incoming{ChatID: strconv.FormatInt(u.Message.Chat.ID, 10), UserID: strconv.FormatInt(u.Message.From.ID, 10),
				UserName: u.Message.From.name(), Text: botCommand(u.Message.Text)})
		case u.Callback != nil && u.Callback.Message != nil:
			out = append(out, Incoming{ChatID: strconv.FormatInt(u.Callback.Message.Chat.ID, 10), UserID: strconv.FormatInt(u.Callback.From.ID, 10),
				UserName: u.Callback.From.name(), Action: u.Callback.Data, ActionRef: u.Callback.ID})
		}
	}
	return out, cursor, nil
}

// botCommand drops the "@BotName" suffix Telegram adds to commands in groups ("/task@akili_bot").
func botCommand(text string) string {
	if !strings.HasPrefix(text, "/") {
		return text
	}
	cmd, rest, _ := strings.Cut(text, " ")
	if i := strings.Index(cmd, "@"); i > 0 {
		cmd = cmd[:i]
	}
	return strings.TrimSpace(cmd + " " + rest)
}

// Send implements Platform.
func (t *Telegram) Send(ctx context.Context, chatID string, msg Outgoing) error {
	parts := chunks(msg.Text)
	for i, p := range parts {
		body := map[string]any{"chat_id": chatID, "text": p, "disable_web_page_preview": true}
		if i == len(parts)-1 && len(msg.Buttons) > 0 {
			row := make([]map[string]string, len(msg.Buttons))
			for j, b := range msg.Buttons {
				row[j] = map[string]string{"text": b.Label, "callback_data": b.Data}
			}
			body["reply_markup"] = map[string]any{"inline_keyboard": [][]map[string]string{row}}
		}
		if err := t.call(ctx, "sendMessage", body, nil); err != nil {
			return err
		}
	}
	return nil
}

// Ack implements Acker.
func (t *Telegram) Ack(ctx context.Context, ref, text string) error {
	return t.call(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": ref, "text": text}, nil)
}
