// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type taskRun struct {
	id    string
	file  string
	agent *sim
	final *apiTask
}

var terminal = map[string]bool{"succeeded": true, "failed": true, "cancelled": true, "timed_out": true}

// runTasks queues cfg.tasks write tasks, pinned round-robin to agents or matched by label, waits for them to finish and
// measures latency from the control plane's own timestamps, so polling cadence does not skew it.
func runTasks(ctx context.Context, cfg *config, api *apiClient, sims []*sim, rep *report) {
	byAgent := map[string]*sim{}
	for _, s := range sims {
		byAgent[s.st.AgentID] = s
	}
	progress("creating %d tasks", cfg.tasks)
	runs := make([]*taskRun, cfg.tasks)
	var createLat []time.Duration
	var createErrs []string
	var mu sync.Mutex
	jobs := make(chan int)
	var wg sync.WaitGroup
	t0 := time.Now()
	for w := 0; w < cfg.taskConcurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				s := sims[i%len(sims)]
				file := fmt.Sprintf("%s-%d.txt", cfg.prefix, i)
				body := map[string]any{"title": "loadtest " + file, "goal": "write: " + file + " :: ok", "autonomy": cfg.autonomy}
				if cfg.taskSelector {
					body["selector"] = []string{"loadtest=" + cfg.prefix}
				} else {
					body["agent_id"] = s.st.AgentID
				}
				var t apiTask
				start := time.Now()
				err := api.retry(ctx, http.MethodPost, "/tasks", body, &t)
				d := time.Since(start)
				mu.Lock()
				if err != nil {
					createErrs = append(createErrs, err.Error())
				} else {
					createLat = append(createLat, d)
					runs[i] = &taskRun{id: t.ID, file: file, agent: s}
				}
				mu.Unlock()
			}
		}()
	}
	for i := 0; i < cfg.tasks && ctx.Err() == nil; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	createDur := time.Since(t0)

	pending := map[string]*taskRun{}
	for _, r := range runs {
		if r != nil {
			pending[r.id] = r
		}
	}
	progress("waiting for %d tasks", len(pending))
	deadline := time.Now().Add(cfg.taskTimeout)
	for len(pending) > 0 && time.Now().Before(deadline) && ctx.Err() == nil {
		pollTasks(ctx, api, pending)
		if len(pending) > 0 {
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
			}
		}
	}
	allDone := time.Since(t0)

	var queueLat, totalLat []time.Duration
	status := map[string]int{}
	var failures []string
	verified := 0
	for _, r := range runs {
		if r == nil {
			continue
		}
		if r.final == nil {
			status["unfinished"]++
			continue
		}
		t := r.final
		status[t.Status]++
		if t.StartedAt != nil {
			queueLat = append(queueLat, t.StartedAt.Sub(t.CreatedAt))
		}
		if t.FinishedAt != nil {
			totalLat = append(totalLat, t.FinishedAt.Sub(t.CreatedAt))
		}
		if t.Status != "succeeded" {
			if len(failures) < 5 {
				failures = append(failures, fmt.Sprintf("%s %s: %s %s", t.ID, t.Status, t.StatusReason, t.Error))
			}
			continue
		}
		ran := r.agent
		if t.AssignedAgentID != nil && byAgent[*t.AssignedAgentID] != nil {
			ran = byAgent[*t.AssignedAgentID]
		}
		if b, err := os.ReadFile(filepath.Join(ran.st.Workdir, r.file)); err == nil && strings.TrimSpace(string(b)) == "ok" {
			verified++
		}
	}
	keys := make([]string, 0, len(status))
	for k := range status {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %d", k, status[k]))
	}
	rep.add("tasks              %d requested, %d created in %s, all settled after %s", cfg.tasks, len(createLat), ms(createDur), ms(allDone))
	rep.add("task create        %s", dist(createLat))
	rep.add("task outcome       %s; %d files verified on disk", strings.Join(parts, ", "), verified)
	rep.add("task queue wait    %s", dist(queueLat))
	rep.add("task completion    %s", dist(totalLat))
	for i, e := range createErrs {
		if i == 3 {
			rep.add("  ... %d more create errors", len(createErrs)-3)
			break
		}
		rep.add("  create error     %s", e)
	}
	for _, f := range failures {
		rep.add("  failure          %s", f)
	}
}

// pollTasks settles pending tasks: one list call covers the newest 200 (the largest page), the rest are fetched one
// by one.
func pollTasks(ctx context.Context, api *apiClient, pending map[string]*taskRun) {
	var list []apiTask
	if err := api.call(ctx, http.MethodGet, "/tasks?size=200", nil, &list); err == nil {
		seen := map[string]bool{}
		for i := range list {
			t := &list[i]
			seen[t.ID] = true
			if r, ok := pending[t.ID]; ok && terminal[t.Status] {
				r.final = t
				delete(pending, t.ID)
			}
		}
		var missing []string
		for id := range pending {
			if !seen[id] {
				missing = append(missing, id)
			}
		}
		if len(missing) == 0 {
			return
		}
		pollEach(ctx, api, pending, missing)
		return
	}
	ids := make([]string, 0, len(pending))
	for id := range pending {
		ids = append(ids, id)
	}
	pollEach(ctx, api, pending, ids)
}

func pollEach(ctx context.Context, api *apiClient, pending map[string]*taskRun, ids []string) {
	var mu sync.Mutex
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			var t apiTask
			if api.call(ctx, http.MethodGet, "/tasks/"+id, nil, &t) != nil || !terminal[t.Status] {
				return
			}
			mu.Lock()
			pending[id].final = &t
			delete(pending, id)
			mu.Unlock()
		}()
	}
	wg.Wait()
}
