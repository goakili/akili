// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command akili-loadtest simulates a fleet of agents against one control plane. Every simulated
// agent is a real link.Agent with its own Ed25519 identity, so the control plane sees real signed
// handshakes, tunnels, heartbeats, presence writes and task sessions.
//
// Each agent holds about three file descriptors in this process (tunnel socket, git-proxy
// listener, plus transient ones) and two in the server (tunnel socket, Redis command
// subscription). Raise the limit first, e.g. `ulimit -n 8192` for 1000 agents: many shells
// default to 256 (macOS) or 1024 (Linux).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/goakili/akili/agent/internal/host"
	"github.com/goakili/akili/agent/internal/link"
	"github.com/jkaninda/logger"
)

type config struct {
	url                string
	agents             int
	tasks              int
	workDir            string
	prefix             string
	prepareConcurrency int
	rampRate           float64
	hold               time.Duration
	onlineTimeout      time.Duration
	taskConcurrency    int
	taskTimeout        time.Duration
	taskSelector       bool
	policy             string
	autonomy           int
	cleanup            bool
	serverPID          int
	caCert             string
	insecure           bool
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return v
	}
	return def
}

func main() {
	cfg := &config{}
	flag.StringVar(&cfg.url, "url", env("AKILI_URL", "http://127.0.0.1:8080"), "control plane URL (AKILI_URL)")
	flag.IntVar(&cfg.agents, "agents", envInt("AGENTS", 1000), "number of simulated agents (AGENTS)")
	flag.IntVar(&cfg.tasks, "tasks", envInt("TASKS", 0), "tasks to run across the agents once they are online; 0 skips (TASKS)")
	flag.StringVar(&cfg.workDir, "work-dir", env("AKILI_LOADTEST_DIR", "./loadtest-state"), "directory holding one state dir per agent; reused across runs")
	flag.StringVar(&cfg.prefix, "prefix", "lt", "agent name prefix (<prefix>-0001) and task file prefix")
	flag.IntVar(&cfg.prepareConcurrency, "prepare-concurrency", 16, "parallel create+enroll calls")
	flag.Float64Var(&cfg.rampRate, "ramp", 100, "new connections per second during ramp-up")
	flag.DurationVar(&cfg.hold, "hold", 60*time.Second, "steady-state window with every agent connected")
	flag.DurationVar(&cfg.onlineTimeout, "online-timeout", 5*time.Minute, "how long to wait for the fleet to come online")
	flag.IntVar(&cfg.taskConcurrency, "task-concurrency", 16, "parallel task creations")
	flag.DurationVar(&cfg.taskTimeout, "task-timeout", 5*time.Minute, "how long to wait for all tasks to finish")
	flag.BoolVar(&cfg.taskSelector, "task-selector", false, "let the dispatcher pick any agent by label instead of pinning each task to one")
	flag.StringVar(&cfg.policy, "policy", "full-no-approval", "policy bound to the agents (tasks must run without approval)")
	flag.IntVar(&cfg.autonomy, "autonomy", 3, "agent and task autonomy level")
	flag.BoolVar(&cfg.cleanup, "cleanup", false, "delete the agents and their state dirs at the end")
	flag.IntVar(&cfg.serverPID, "server-pid", envInt("AKILI_SERVER_PID", 0), "control-plane PID, to report its RSS (same host only)")
	flag.StringVar(&cfg.caCert, "ca-cert", env("AKILI_CA_CERT", ""), "extra CA bundle to trust")
	flag.BoolVar(&cfg.insecure, "insecure", env("AKILI_INSECURE", "") == "true", "skip TLS verification (development only)")
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: akili-loadtest [flags]

Auth: AKILI_API_KEY (admin-scoped), or AKILI_ADMIN_EMAIL and AKILI_ADMIN_PASSWORD.
Raise the file descriptor limit first: ulimit -n 8192 (about 3 per agent here, 2 in the server).

`)
		flag.PrintDefaults()
	}
	flag.Parse()
	cfg.url = strings.TrimRight(cfg.url, "/")
	logger.New(logger.WithLevel(logger.LogLevel(env("AKILI_LOG_LEVEL", "error"))))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, cfg); err != nil {
		fmt.Fprintln(os.Stderr, "akili-loadtest:", err)
		os.Exit(1)
	}
}

type report struct {
	lines []string
}

func (r *report) add(format string, args ...any) {
	r.lines = append(r.lines, fmt.Sprintf(format, args...))
}

func progress(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[%s] %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
}

func run(ctx context.Context, cfg *config) error {
	if cfg.agents <= 0 {
		return errors.New("-agents must be positive")
	}
	abs, err := filepath.Abs(cfg.workDir)
	if err != nil {
		return err
	}
	cfg.workDir = abs
	dialer, err := link.Dialer(cfg.caCert, cfg.insecure)
	if err != nil {
		return err
	}
	tr := &http.Transport{TLSClientConfig: dialer.TLSClientConfig, MaxIdleConns: 256, MaxIdleConnsPerHost: 64, IdleConnTimeout: 30 * time.Second}
	httpc := &http.Client{Timeout: 60 * time.Second, Transport: tr}
	api := newAPIClient(cfg.url, os.Getenv("AKILI_API_KEY"), tr)
	if api.key == "" {
		email, pass := os.Getenv("AKILI_ADMIN_EMAIL"), os.Getenv("AKILI_ADMIN_PASSWORD")
		if email == "" || pass == "" {
			return errors.New("set AKILI_API_KEY, or AKILI_ADMIN_EMAIL and AKILI_ADMIN_PASSWORD")
		}
		if err := api.login(ctx, email, pass); err != nil {
			return fmt.Errorf("login: %w", err)
		}
	}
	policyID, err := api.policyID(ctx, cfg.policy)
	if err != nil {
		return err
	}
	rep := &report{}
	rep.add("control plane      %s", cfg.url)
	serverRSS := func(label string) {
		if v, ok := rssMB(cfg.serverPID); ok {
			rep.add("server RSS         %-28s %.0f MB", label, v)
		}
	}
	serverRSS("before connect")

	// Facts are gathered once and shared: every agent reports the same host, as on one machine.
	facts := host.Facts("loadtest", "")

	progress("preparing %d agents in %s", cfg.agents, cfg.workDir)
	t0 := time.Now()
	sims, ps, err := prepare(ctx, cfg, api, httpc, policyID, facts)
	if err != nil {
		return err
	}
	rep.add("prepare            %d ready in %s: %d reused, %d created, %d re-enrolled, %d failed, %d enroll 429s",
		len(sims), ms(time.Since(t0)), ps.reused.Load(), ps.created.Load(), ps.reenrolled.Load(), ps.failed.Load(), enrollThrottled.Load())
	if e, _ := ps.firstErr.Load().(string); e != "" {
		rep.add("  first failure    %s", e)
	}
	if len(sims) == 0 {
		fmt.Println(strings.Join(rep.lines, "\n"))
		return errors.New("no agent could be prepared")
	}
	progress("prepared %d agents (%d reused, %d created) in %s", len(sims), ps.reused.Load(), ps.created.Load(), ms(time.Since(t0)))

	m := newMetrics()
	simCtx, stopSims := context.WithCancel(ctx)
	defer stopSims()
	var wg sync.WaitGroup
	interval := time.Duration(float64(time.Second) / cfg.rampRate)
	progress("connecting %d agents at %.0f/s", len(sims), cfg.rampRate)
	rampStart := time.Now()
	for _, s := range sims {
		wg.Add(1)
		go func() { defer wg.Done(); s.run(simCtx, dialer, m) }()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
	rampDur := time.Since(rampStart)
	allUp := waitFor(ctx, cfg.onlineTimeout, 200*time.Millisecond, func() bool { return m.online.Load() >= int64(len(sims)) })
	tunnelsUp := time.Since(rampStart)
	progress("%d/%d tunnels up after %s", m.online.Load(), len(sims), ms(tunnelsUp))

	var listLat []time.Duration
	var minOnline = int64(len(sims))
	names := map[string]bool{}
	for _, s := range sims {
		names[s.st.AgentID] = true
	}
	countOnline := func() (int64, error) {
		as, d, err := api.agents(ctx)
		if err != nil {
			return -1, err
		}
		listLat = append(listLat, d)
		var n int64
		for _, a := range as {
			if names[a.ID] && a.Status == "online" {
				n++
			}
		}
		return n, nil
	}
	var apiOnline int64
	apiAllOnline := waitFor(ctx, cfg.onlineTimeout, 2*time.Second, func() bool {
		apiOnline, _ = countOnline()
		return apiOnline >= int64(len(sims))
	})
	apiUp := time.Since(rampStart)
	progress("control plane reports %d/%d online after %s", apiOnline, len(sims), ms(apiUp))
	serverRSS("fleet connected")

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	rep.add("ramp-up            %d dials started in %s (target %.0f/s)", len(sims), ms(rampDur), cfg.rampRate)
	rep.add("tunnels up         %d/%d after %s%s", m.online.Load(), len(sims), ms(tunnelsUp), timeoutNote(allUp))
	rep.add("API online         %d/%d after %s%s", apiOnline, len(sims), ms(apiUp), timeoutNote(apiAllOnline))

	m.phase.Store(phaseSteady)
	if cfg.hold > 0 {
		progress("steady state for %s", cfg.hold)
		end := time.Now().Add(cfg.hold)
		for time.Now().Before(end) && ctx.Err() == nil {
			select {
			case <-ctx.Done():
			case <-time.After(min(10*time.Second, time.Until(end))):
			}
			if n, err := countOnline(); err == nil && n < minOnline {
				minOnline = n
			}
		}
		rep.add("steady state       %s, lowest API online count %d/%d, tunnels up at end %d", cfg.hold, minOnline, len(sims), m.online.Load())
	}

	if cfg.tasks > 0 && ctx.Err() == nil {
		m.phase.Store(phaseTasks)
		runTasks(ctx, cfg, api, sims, rep)
		serverRSS("after tasks")
	}
	finalOnline, _ := countOnline()
	rep.add("API online at end  %d/%d", finalOnline, len(sims))

	m.mu.Lock()
	rep.add("connect latency    %s", dist(m.firstConnect))
	if len(m.reconnect) > 0 {
		rep.add("reconnect latency  %s", dist(m.reconnect))
	}
	if len(m.connectErrs) == 0 {
		rep.add("connect errors     none")
	} else {
		keys := make([]string, 0, len(m.connectErrs))
		for k := range m.connectErrs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			rep.add("connect errors     %-40s %d", k, m.connectErrs[k])
		}
	}
	for p := 0; p < phaseCount; p++ {
		rep.add("disconnects        %-40s %d", phaseNames[p], m.lostByPhase[p])
	}
	for _, s := range m.lostSample {
		rep.add("  e.g.             %s", s)
	}
	m.mu.Unlock()
	rep.add("GET /agents        %s", dist(listLat))
	rep.add("loadtest process   RSS %s, heap %.1f MB, stacks %.1f MB, %d goroutines (%.1f KB heap+stack per agent)",
		selfRSS(), float64(mem.HeapInuse)/(1<<20), float64(mem.StackInuse)/(1<<20), runtime.NumGoroutine(),
		float64(mem.HeapInuse+mem.StackInuse)/1024/float64(len(sims)))
	serverRSS("end")

	stopSims()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		progress("some agents did not stop within 15s")
	}
	if cfg.cleanup {
		cleanup(context.WithoutCancel(ctx), cfg, api, sims, rep)
	}

	fmt.Println()
	fmt.Println("==== akili-loadtest summary ====")
	fmt.Println(strings.Join(rep.lines, "\n"))
	return nil
}

func timeoutNote(ok bool) string {
	if ok {
		return ""
	}
	return " (TIMED OUT)"
}

// waitFor polls cond until it holds or timeout passes, and reports whether it held.
func waitFor(ctx context.Context, timeout, every time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return true
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return false
		}
		select {
		case <-ctx.Done():
		case <-time.After(every):
		}
	}
}

func cleanup(ctx context.Context, cfg *config, api *apiClient, sims []*sim, rep *report) {
	progress("deleting %d agents", len(sims))
	var deleted, failed atomic.Int64
	jobs := make(chan *sim)
	var wg sync.WaitGroup
	for w := 0; w < cfg.prepareConcurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for s := range jobs {
				if err := api.retry(ctx, http.MethodDelete, "/agents/"+s.st.AgentID, nil, nil); err != nil && statusOf(err) != http.StatusNotFound {
					failed.Add(1)
					continue
				}
				_ = os.RemoveAll(s.dir)
				deleted.Add(1)
			}
		}()
	}
	for _, s := range sims {
		jobs <- s
	}
	close(jobs)
	wg.Wait()
	rep.add("cleanup            %d agents deleted, %d failed", deleted.Load(), failed.Load())
}
