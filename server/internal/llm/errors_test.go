// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

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
