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

package llm

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		err               error
		rejected, tooLong bool
	}{
		{&StatusError{Code: 529, Msg: "overloaded"}, false, false},
		{&StatusError{Code: 429, Msg: "rate limited"}, false, false},
		{&StatusError{Code: 401, Msg: "bad key"}, true, false},
		{&StatusError{Code: 400, Msg: "This model's maximum context length is 128000 tokens"}, true, true},
		{fmt.Errorf("wrapped: %w", &StatusError{Code: 413, Msg: "request too large"}), true, true},
		{errors.New("connection reset by peer"), false, false},
	} {
		if Rejected(tc.err) != tc.rejected || ContextTooLong(tc.err) != tc.tooLong {
			t.Errorf("%v: rejected=%v tooLong=%v, want %v %v", tc.err, Rejected(tc.err), ContextTooLong(tc.err), tc.rejected, tc.tooLong)
		}
	}
}

func TestHistoryTokens(t *testing.T) {
	if got := HistoryTokens(0, 32_000, 30_000); got != DefaultContextTokens-32_000-10_000-2_000 {
		t.Errorf("default window: %d", got)
	}
	if got := HistoryTokens(1_000_000, 64_000, 0); got != 1_000_000-64_000-2_000 {
		t.Errorf("1M window: %d", got)
	}
	if got := HistoryTokens(8_000, 4_000, 0); got != minHistoryTokens {
		t.Errorf("tiny window: %d", got)
	}
}
