// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package chat

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/goakili/akili/server/internal/chat/chattest"
)

func sign(secret string, ts int64, body []byte) http.Header {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte("v0:" + strconv.FormatInt(ts, 10) + ":"))
	m.Write(body)
	h := http.Header{}
	h.Set("X-Slack-Request-Timestamp", strconv.FormatInt(ts, 10))
	h.Set("X-Slack-Signature", "v0="+hex.EncodeToString(m.Sum(nil)))
	return h
}

func TestSlackVerify(t *testing.T) {
	s := NewSlack("", "xoxb", "signing-secret")
	body := []byte(`{"type":"event_callback"}`)
	now := time.Now()
	if err := s.Verify(sign("signing-secret", now.Unix(), body), body, now); err != nil {
		t.Fatalf("valid signature refused: %v", err)
	}
	if s.Verify(sign("wrong", now.Unix(), body), body, now) == nil {
		t.Fatal("wrong secret accepted")
	}
	if s.Verify(sign("signing-secret", now.Add(-10*time.Minute).Unix(), body), body, now) == nil {
		t.Fatal("replayed (old) request accepted")
	}
	if s.Verify(sign("signing-secret", now.Unix(), body), []byte(`{"type":"tampered"}`), now) == nil {
		t.Fatal("tampered body accepted")
	}
	if NewSlack("", "x", "").Verify(sign("", now.Unix(), body), body, now) == nil {
		t.Fatal("empty signing secret accepted")
	}
}

func TestSlackParse(t *testing.T) {
	ev, _ := ParseEvent([]byte(`{"type":"url_verification","challenge":"abc"}`))
	if ev.Challenge != "abc" {
		t.Fatal("challenge")
	}
	ev, _ = ParseEvent([]byte(`{"type":"event_callback","event_id":"E1","event":{"type":"app_mention","user":"U1","text":"<@UBOT> deploy api","channel":"C1"}}`))
	if ev.Message == nil || ev.Message.Text != "deploy api" || ev.Message.ChatID != "C1" || ev.EventID != "E1" {
		t.Fatalf("mention: %+v", ev.Message)
	}
	ev, _ = ParseEvent([]byte(`{"type":"event_callback","event":{"type":"message","channel_type":"im","bot_id":"B1","text":"echo","channel":"D1"}}`))
	if ev.Message != nil {
		t.Fatal("the bot's own message was handled (loop)")
	}
	ev, _ = ParseEvent([]byte(`{"type":"event_callback","event":{"type":"message","channel_type":"channel","user":"U1","text":"chatter","channel":"C1"}}`))
	if ev.Message != nil {
		t.Fatal("a channel message without a mention was handled")
	}
	payload, _ := json.Marshal(map[string]any{"type": "block_actions", "user": map[string]string{"id": "U1"}, "channel": map[string]string{"id": "C1"},
		"actions": []map[string]string{{"value": "ap:apr_1:approve"}}})
	in, err := ParseAction([]byte("payload=" + url.QueryEscape(string(payload))))
	if err != nil || in.Action != "ap:apr_1:approve" || in.UserID != "U1" {
		t.Fatalf("action: %+v %v", in, err)
	}
}

func TestHelpers(t *testing.T) {
	if botCommand("/task@akili_bot deploy") != "/task deploy" || botCommand("hello @x") != "hello @x" {
		t.Fatal("botCommand")
	}
	if id, verb, ok := parseApprovalAction("ap:apr_1:deny"); !ok || id != "apr_1" || verb != "deny" {
		t.Fatal("parseApprovalAction")
	}
	if _, _, ok := parseApprovalAction("ap:apr_1:sudo"); ok {
		t.Fatal("unknown verb accepted")
	}
	long := strings.Repeat("line of text\n", 1000)
	parts := chunks(long)
	if len(parts) < 3 || strings.Join(parts, "") != long {
		t.Fatalf("chunks: %d parts", len(parts))
	}
	for _, p := range parts {
		if len([]rune(p)) > maxChunk {
			t.Fatal("chunk too long")
		}
	}
	if textCommand("ap:apr_9:approve") != "/approve apr_9" {
		t.Fatal("textCommand")
	}
}

func TestClientsAgainstFake(t *testing.T) {
	f := chattest.New("123:tok", "xoxb-1", "+1555")
	srv := httptest.NewServer(f)
	defer srv.Close()
	ctx := context.Background()

	tg := NewTelegram(srv.URL, "123:tok")
	if who, err := tg.Test(ctx); err != nil || who != "@akili_test_bot" {
		t.Fatalf("telegram test: %q %v", who, err)
	}
	if _, err := NewTelegram(srv.URL, "123:wrong").Test(ctx); err == nil || strings.Contains(err.Error(), "123:wrong") {
		t.Fatalf("bad token: %v (the token must not appear in errors)", err)
	}
	post := func(path string, v any) {
		b, _ := json.Marshal(v)
		resp, err := http.Post(srv.URL+path, "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	post("/_fake/telegram", chattest.Inject{Chat: "42", User: "7", Username: "jo", Text: "/status@akili_test_bot"})
	post("/_fake/telegram", chattest.Inject{Chat: "42", User: "7", Username: "jo", Callback: "ap:apr_1:approve"})
	msgs, cursor, err := tg.Poll(ctx, 0)
	if err != nil || len(msgs) != 2 || msgs[0].Text != "/status" || msgs[0].UserID != "7" || msgs[1].Action != "ap:apr_1:approve" || cursor != 2 {
		t.Fatalf("poll: %+v %d %v", msgs, cursor, err)
	}
	if msgs, _, _ := tg.Poll(ctx, cursor); len(msgs) != 0 {
		t.Fatal("poll returned old updates after the cursor")
	}
	if err := tg.Send(ctx, "42", Outgoing{Text: "approve?", Buttons: approvalButtons("apr_1")}); err != nil {
		t.Fatal(err)
	}

	sl := NewSlack(srv.URL+"/api", "xoxb-1", "s")
	if _, err := sl.Test(ctx); err != nil {
		t.Fatal(err)
	}
	if err := sl.Send(ctx, "C1", Outgoing{Text: "hello"}); err != nil {
		t.Fatal(err)
	}

	sg := NewSignal(srv.URL, "+1555")
	post("/_fake/signal", chattest.Inject{Chat: "+4911", User: "uuid-1", Username: "Ann", Text: "/approve apr_1"})
	smsgs, _, err := sg.Poll(ctx, 0)
	if err != nil || len(smsgs) != 1 || smsgs[0].ChatID != "+4911" || smsgs[0].UserID != "uuid-1" {
		t.Fatalf("signal poll: %+v %v", smsgs, err)
	}
	if err := sg.Send(ctx, "+4911", Outgoing{Text: "approve?", Buttons: approvalButtons("apr_2")}); err != nil {
		t.Fatal(err)
	}

	resp, _ := http.Get(srv.URL + "/_fake/sent")
	var sent []chattest.Sent
	_ = json.NewDecoder(resp.Body).Decode(&sent)
	resp.Body.Close()
	if len(sent) != 3 || len(sent[0].Buttons) != 2 || sent[1].Platform != "slack" || !strings.Contains(sent[2].Text, "/approve apr_2") {
		t.Fatalf("sent: %+v", sent)
	}
}
