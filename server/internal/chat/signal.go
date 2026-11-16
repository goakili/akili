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

package chat

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Signal talks to a signal-cli REST API server; Signal has no buttons, so approvals use /approve and /deny.
type Signal struct {
	base    string
	account string // the bot's number, e.g. +15550001111
}

func NewSignal(base, account string) *Signal {
	return &Signal{base: strings.TrimRight(base, "/"), account: account}
}

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

// Poll implements Poller; signal-cli hands each message out once, so there is no cursor.
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

// Send implements Platform; buttons become text commands.
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
	if id, choice, ok := parseQuestionAction(data); ok {
		return fmt.Sprintf("/answer %s %d", id, choice+1)
	}
	return data
}
