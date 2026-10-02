// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Phases a disconnect can fall into.
const (
	phaseRamp = iota
	phaseSteady
	phaseTasks
	phaseCount
)

var phaseNames = [phaseCount]string{"ramp-up", "steady state", "task run"}

type metrics struct {
	online atomic.Int64
	phase  atomic.Int32

	mu             sync.Mutex
	firstConnect   []time.Duration
	reconnect      []time.Duration
	connectErrs    map[string]int
	firstAttemptOK int
	lostByPhase    [phaseCount]int
	lostSample     []string
}

func newMetrics() *metrics { return &metrics{connectErrs: map[string]int{}} }

func (m *metrics) connected(d time.Duration, first bool) {
	m.online.Add(1)
	m.mu.Lock()
	defer m.mu.Unlock()
	if first {
		m.firstConnect = append(m.firstConnect, d)
	} else {
		m.reconnect = append(m.reconnect, d)
	}
}

func (m *metrics) connectFailed(status int, err error, first bool) {
	key := errKind(status, err)
	if !first {
		key = "reconnect: " + key
	}
	m.mu.Lock()
	m.connectErrs[key]++
	m.mu.Unlock()
}

func (m *metrics) lost(name string, err error) {
	p := m.phase.Load()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lostByPhase[p]++
	if len(m.lostSample) < 5 {
		m.lostSample = append(m.lostSample, fmt.Sprintf("%s during %s: %v", name, phaseNames[p], err))
	}
}

func errKind(status int, err error) string {
	if status != 0 {
		return fmt.Sprintf("HTTP %d", status)
	}
	var ne net.Error
	switch {
	case errors.As(err, &ne) && ne.Timeout():
		return "timeout"
	case strings.Contains(err.Error(), "connection refused"):
		return "connection refused"
	case strings.Contains(err.Error(), "too many open files"):
		return "too many open files (raise ulimit -n)"
	case strings.Contains(err.Error(), "connection reset"):
		return "connection reset"
	}
	s := err.Error()
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

// pct returns the nearest-rank percentile of ds (sorted in place).
func pct(ds []time.Duration, p float64) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
	i := int(float64(len(ds))*p/100+0.5) - 1
	if i < 0 {
		i = 0
	}
	if i >= len(ds) {
		i = len(ds) - 1
	}
	return ds[i]
}

func dist(ds []time.Duration) string {
	if len(ds) == 0 {
		return "n/a"
	}
	c := append([]time.Duration(nil), ds...)
	return fmt.Sprintf("p50 %s  p95 %s  p99 %s  max %s  (n=%d)", ms(pct(c, 50)), ms(pct(c, 95)), ms(pct(c, 99)), ms(c[len(c)-1]), len(c))
}

func ms(d time.Duration) string {
	switch {
	case d >= 10*time.Second:
		return fmt.Sprintf("%.1fs", d.Seconds())
	case d >= time.Second:
		return fmt.Sprintf("%.2fs", d.Seconds())
	default:
		return fmt.Sprintf("%.1fms", float64(d.Microseconds())/1000)
	}
}

// rssMB reads a process's resident set size through ps, which works on Linux and macOS alike.
func rssMB(pid int) (float64, bool) {
	if pid <= 0 {
		return 0, false
	}
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, false
	}
	kb, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		return 0, false
	}
	return kb / 1024, true
}

func selfRSS() string {
	if v, ok := rssMB(os.Getpid()); ok {
		return fmt.Sprintf("%.0f MB", v)
	}
	return "n/a"
}
