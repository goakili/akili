// Copyright (c) 2026 Jonas Kaninda.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
