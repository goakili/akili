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

package coder

import "testing"

func TestValidSender(t *testing.T) {
	for s, ok := range map[string]bool{
		"Akili <akili@example.com>":   true,
		"akili@example.com":           true,
		"":                            false,
		"not an address":              false,
		"Akili <a@b.c>\nBcc: x@y.z":   false,
		"Akili <a@b.c>\r\nBcc: x@y.z": false,
		"a@b.c, ceo@example.com":      false,
	} {
		if err := validSender(s); (err == nil) != ok {
			t.Errorf("%q: ok=%v err=%v", s, err == nil, err)
		}
	}
}
