// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package chat

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// Signal talks to a signal-cli REST API server (github.com/bbernhard/signal-cli-rest-api) that holds
// the bot's registered number. Signal has no buttons: approvals use /approve and /deny.
type Signal struct {
	base    string
	account string // the bot's number, e.g. +15550001111
}

// NewSignal returns a client.
func NewSignal(base, account string) *Signal {
	return &Signal{base: strings.TrimRight(base, "/"), account: account}
}

// Test implements Platform.
func (s *Signal) Test(ctx context.Context) (string, error) {
	if s.base == "" || s.account == "" {
		return "", errors.New("signal needs the REST server URL and the account number")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base+"/v1/about", nil)
	if err != nil {
		return "", err
	}
	var about struct {
		Mode string `json:"mode"`
	}
	if err := do(req, &about); err != nil {
		return "", err
	}
	return s.account + " (signal-cli " + about.Mode + ")", nil
}

type signalEnvelope struct {
	Envelope struct {
		Source       string `json:"source"`
		SourceNumber string `json:"sourceNumber"`
		SourceUUID   string `json:"sourceUuid"`
		SourceName   string `json:"sourceName"`
		DataMessage  *struct {
			Message   string `json:"message"`
			GroupInfo *struct {
				GroupID string `json:"groupId"`
			} `json:"groupInfo"`
		} `json:"dataMessage"`
	} `json:"envelope"`
}

// Poll implements Poller. signal-cli hands each message out once, so there is no cursor.
func (s *Signal) Poll(ctx context.Context, cursor int64) ([]Incoming, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base+"/v1/receive/"+url.PathEscape(s.account)+"?timeout=10", nil)
	if err != nil {
		return nil, cursor, err
	}
	var envs []signalEnvelope
	if err := do(req, &envs); err != nil {
		return nil, cursor, err
	}
	var out []Incoming
	for _, e := range envs {
		env := e.Envelope
		if env.DataMessage == nil || strings.TrimSpace(env.DataMessage.Message) == "" {
			continue
		}
		user := env.SourceUUID
		if user == "" {
			user = env.SourceNumber
		}
		if user == "" {
			user = env.Source
		}
		chat := env.SourceNumber
		if chat == "" {
			chat = env.Source
		}
		if g := env.DataMessage.GroupInfo; g != nil && g.GroupID != "" {
			// signal-cli-rest-api addresses groups as "group." + base64 of the group id.
			chat = "group." + base64.StdEncoding.EncodeToString([]byte(g.GroupID))
		}
		name := env.SourceName
		if name == "" {
			name = env.SourceNumber
		}
		out = append(out, Incoming{ChatID: chat, UserID: user, UserName: name, Text: strings.TrimSpace(env.DataMessage.Message)})
	}
	return out, cursor, nil
}

// Send implements Platform. Buttons become text commands.
func (s *Signal) Send(ctx context.Context, chatID string, msg Outgoing) error {
	text := msg.Text
	if len(msg.Buttons) > 0 {
		var cmds []string
		for _, b := range msg.Buttons {
			cmds = append(cmds, textCommand(b.Data))
		}
		text += "\n\nReply " + strings.Join(cmds, " or ")
	}
	for _, p := range chunks(text) {
		if err := postJSON(ctx, s.base+"/v2/send", nil, map[string]any{"message": p, "number": s.account, "recipients": []string{chatID}}, nil); err != nil {
			return err
		}
	}
	return nil
}

// textCommand turns button data ("ap:<id>:approve") into the equivalent command.
func textCommand(data string) string {
	if id, verb, ok := parseApprovalAction(data); ok {
		return "/" + verb + " " + id
	}
	return data
}
