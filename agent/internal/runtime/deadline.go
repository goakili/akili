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

package runtime

import (
	"context"
	"sync"
	"time"
)

// deadline pauses while waiting on a person, as the control plane does; a nil deadline (chat) does nothing.
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

// resume restarts the clock at next, the control plane's deadline, or else pushes the old one back by the wait.
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
