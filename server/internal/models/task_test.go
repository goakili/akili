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
