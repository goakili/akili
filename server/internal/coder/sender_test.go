// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

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
