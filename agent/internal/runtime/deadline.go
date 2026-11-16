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
