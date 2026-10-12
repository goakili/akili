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
