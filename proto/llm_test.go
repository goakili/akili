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
