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

package runtime

import (
	"context"
	"sync"
	"time"
)

// deadline ends a task's context when its time is up. The clock stops while the agent waits on a
// person (an approval or an ask_user answer): the control plane does not count that time either, and
// sends the deadline it computed when the wait ends. A nil deadline (chat sessions) does nothing.
type deadline struct {
	mu     sync.Mutex
	at     time.Time
	paused time.Time
	timer  *time.Timer
	cancel context.CancelCauseFunc
}

// withDeadline returns a context that is cancelled with context.DeadlineExceeded as its cause at `at`.
func withDeadline(parent context.Context, at time.Time) (context.Context, *deadline, context.CancelFunc) {
	ctx, cancel := context.WithCancelCause(parent)
	d := &deadline{at: at, cancel: cancel}
	d.timer = time.AfterFunc(time.Until(at), func() { cancel(context.DeadlineExceeded) })
	return ctx, d, func() {
		d.timer.Stop()
		cancel(context.Canceled)
	}
}

func (d *deadline) pause() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.paused.IsZero() {
		d.paused = time.Now()
		d.timer.Stop()
	}
}

// resume restarts the clock at next, the control plane's deadline, or else pushes the old deadline
// back by the time spent waiting.
func (d *deadline) resume(next *time.Time) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if next == nil && d.paused.IsZero() {
		return
	}
	switch {
	case next != nil:
		d.at = *next
	case !d.paused.IsZero():
		d.at = d.at.Add(time.Since(d.paused))
	}
	d.paused = time.Time{}
	d.timer.Reset(time.Until(d.at))
}
