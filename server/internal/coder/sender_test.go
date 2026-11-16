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
