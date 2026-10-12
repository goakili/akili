// Copyright (c) 2026 Jonas Kaninda.
//
// This file is part of Akili.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.
//
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
