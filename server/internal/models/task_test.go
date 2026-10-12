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

package models

import (
	"testing"
	"time"
)

func TestTaskDeadlineSkipsTimeWaitingOnAPerson(t *testing.T) {
	start := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	task := Task{TimeoutSec: 3600, StartedAt: &start, PausedSec: 1800}
	if at, ok := task.Deadline(); !ok || !at.Equal(start.Add(90*time.Minute)) {
		t.Fatalf("deadline = %v, %v; want 13:30", at, ok)
	}
	task.PausedAt = &start
	if _, ok := task.Deadline(); ok {
		t.Fatal("a paused task has a deadline")
	}
	if _, ok := (&Task{StartedAt: &start}).Deadline(); ok {
		t.Fatal("a task without a timeout has a deadline")
	}
}
