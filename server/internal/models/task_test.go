// SPDX-FileCopyrightText: 2026 Jonas Kaninda
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
