// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: Apache-2.0

package proto

import "testing"

func TestUserMessageImagesFirstWithoutData(t *testing.T) {
	m := UserMessage{Text: "what is this?", Images: []ImageSource{{AttachmentID: "att_1", MediaType: "image/png", Data: "c2VjcmV0"}}}.Message()
	if m.Role != RoleUser || len(m.Content) != 2 {
		t.Fatalf("got %+v", m)
	}
	img, txt := m.Content[0], m.Content[1]
	if img.Type != BlockImage || img.Source == nil || img.Source.AttachmentID != "att_1" || img.Source.Data != "" {
		t.Fatalf("image block %+v must reference the attachment and carry no bytes", img)
	}
	if txt.Type != BlockText || txt.Text != "what is this?" {
		t.Fatalf("text block %+v", txt)
	}
	if only := (UserMessage{Images: []ImageSource{{AttachmentID: "att_2", MediaType: "image/jpeg"}}}).Message(); len(only.Content) != 1 {
		t.Fatalf("an image-only message has no text block: %+v", only)
	}
}

func TestIsImageType(t *testing.T) {
	for _, ok := range []string{"image/png", "image/jpeg", "image/gif", "image/webp"} {
		if !IsImageType(ok) {
			t.Errorf("%s should be accepted", ok)
		}
	}
	for _, bad := range []string{"image/svg+xml", "text/html", ""} {
		if IsImageType(bad) {
			t.Errorf("%s should be refused", bad)
		}
	}
}
