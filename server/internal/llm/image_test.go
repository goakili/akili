// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/goakili/akili/proto"
)

func imageTurn(data string) proto.Message {
	return proto.Message{Role: proto.RoleUser, Content: []proto.Block{
		{Type: proto.BlockImage, Source: &proto.ImageSource{AttachmentID: "att_1", MediaType: "image/png", Data: data}},
		{Type: proto.BlockText, Text: "what is this?"},
	}}
}

func TestAnthropicSendsResolvedImages(t *testing.T) {
	a := NewAnthropic("test-key", "")
	p, err := a.params(Request{Model: "claude-opus-5-5", System: "sys", Messages: []proto.Message{imageTurn("aGVsbG8=")}})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(p.Messages)
	body := string(b)
	for _, want := range []string{`"type":"image"`, `"media_type":"image/png"`, `"data":"aGVsbG8="`, `"type":"base64"`} {
		if !strings.Contains(body, want) {
			t.Errorf("request is missing %s\n%s", want, body)
		}
	}
	if strings.Index(body, `"type":"image"`) > strings.Index(body, `"what is this?"`) {
		t.Error("the image must come before the text")
	}

	p, _ = a.params(Request{Model: "claude-opus-5-5", Messages: []proto.Message{imageTurn("")}})
	if b, _ := json.Marshal(p.Messages); strings.Contains(string(b), `"type":"image"`) {
		t.Errorf("an unresolved image must not be sent: %s", b)
	}
}

func TestOpenAISendsImagesAsContentParts(t *testing.T) {
	var got struct {
		Messages []map[string]any `json:"messages"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"a cat"},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()

	o := NewOpenAICompatible("", srv.URL+"/v1")
	res, err := o.Stream(context.Background(), Request{Model: "gpt", System: "sys", Messages: []proto.Message{
		imageTurn("aGVsbG8="), proto.TextMessage(proto.RoleAssistant, "a cat"), proto.TextMessage(proto.RoleUser, "thanks"),
	}}, nil)
	if err != nil || res.Message.Text() != "a cat" {
		t.Fatalf("stream: %v %+v", err, res)
	}
	if len(got.Messages) != 4 {
		t.Fatalf("messages: %+v", got.Messages)
	}
	parts, ok := got.Messages[1]["content"].([]any)
	if !ok || len(parts) != 2 {
		t.Fatalf("an image turn must be content parts: %+v", got.Messages[1])
	}
	img, _ := parts[0].(map[string]any)
	url, _ := img["image_url"].(map[string]any)
	if img["type"] != "image_url" || url["url"] != "data:image/png;base64,aGVsbG8=" {
		t.Errorf("image part %+v", img)
	}
	if txt, _ := parts[1].(map[string]any); txt["type"] != "text" || txt["text"] != "what is this?" {
		t.Errorf("text part %+v", parts[1])
	}
	if got.Messages[3]["content"] != "thanks" {
		t.Errorf("a text-only turn stays a plain string: %+v", got.Messages[3])
	}
}

func TestFakeDescribesResolvedImages(t *testing.T) {
	res, err := (&Fake{}).Stream(context.Background(), Request{Messages: []proto.Message{{Role: proto.RoleUser, Content: []proto.Block{
		{Type: proto.BlockImage, Source: &proto.ImageSource{MediaType: "image/png", Data: "aGVsbG8="}},
		{Type: proto.BlockText, Text: "images?"},
	}}}}, nil)
	if err != nil || res.Message.Text() != "images: image/png 5 bytes" {
		t.Fatalf("got %q, %v", res.Message.Text(), err)
	}
}
