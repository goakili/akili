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
