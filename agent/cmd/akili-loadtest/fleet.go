// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/goakili/akili/agent/internal/link"
	"github.com/goakili/akili/agent/internal/state"
	"github.com/goakili/akili/proto"
	"github.com/gorilla/websocket"
)

// sim is one simulated agent: a real link.Agent with its own identity and workdir.
type sim struct {
	idx   int
	name  string
	dir   string
	st    *state.State
	agent *link.Agent
}

type prepareStats struct {
	reused, created, reenrolled, failed atomic.Int64
	firstErr                            atomic.Value
}

// prepare makes n agents ready to connect, reusing state directories whose identity the control
// plane still knows, and creating (or re-enrolling) the rest.
func prepare(ctx context.Context, cfg *config, api *apiClient, httpc *http.Client, policyID string, facts proto.HostFacts) ([]*sim, *prepareStats, error) {
	existing, _, err := api.agents(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list agents: %w", err)
	}
	byID := map[string]apiAgent{}
	byName := map[string]apiAgent{}
	for _, a := range existing {
		byID[a.ID] = a
		if a.Status != "revoked" {
			byName[a.Name] = a
		}
	}
	ps := &prepareStats{}
	sims := make([]*sim, cfg.agents)
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < cfg.prepareConcurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				s, err := prepareOne(ctx, cfg, api, httpc, policyID, facts, i, byID, byName, ps)
				if err != nil {
					ps.failed.Add(1)
					ps.firstErr.CompareAndSwap(nil, fmt.Sprintf("%s-%04d: %v", cfg.prefix, i, err))
					continue
				}
				sims[i] = s
			}
		}()
	}
	for i := 0; i < cfg.agents && ctx.Err() == nil; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	out := sims[:0]
	for _, s := range sims {
		if s != nil {
			out = append(out, s)
		}
	}
	return out, ps, ctx.Err()
}

func prepareOne(ctx context.Context, cfg *config, api *apiClient, httpc *http.Client, policyID string, facts proto.HostFacts,
	i int, byID, byName map[string]apiAgent, ps *prepareStats) (*sim, error) {
	name := fmt.Sprintf("%s-%04d", cfg.prefix, i)
	dir := filepath.Join(cfg.workDir, "agents", name)
	want := map[string]any{"policy_id": policyID, "autonomy": cfg.autonomy}

	if st, err := state.Load(dir); err == nil {
		if a, ok := byID[st.AgentID]; ok && a.Status != "revoked" && a.Status != "pending" {
			if a.PolicyID != policyID || a.Autonomy != cfg.autonomy {
				if err := api.retry(ctx, http.MethodPatch, "/agents/"+a.ID, want, nil); err != nil {
					return nil, fmt.Errorf("update policy: %w", err)
				}
			}
			ps.reused.Add(1)
			return newSim(i, name, dir, st, cfg.url, facts)
		}
	}
	// Stale or missing state: the identity is unusable, so start this slot over.
	if err := os.RemoveAll(dir); err != nil {
		return nil, err
	}
	var enr apiEnrollment
	if a, ok := byName[name]; ok {
		if err := api.retry(ctx, http.MethodPost, "/agents/"+a.ID+"/reenroll", nil, &enr); err != nil {
			return nil, fmt.Errorf("reenroll: %w", err)
		}
		if err := api.retry(ctx, http.MethodPatch, "/agents/"+a.ID, want, nil); err != nil {
			return nil, fmt.Errorf("update policy: %w", err)
		}
		ps.reenrolled.Add(1)
	} else {
		body := map[string]any{"name": name, "labels": []string{"loadtest=" + cfg.prefix}, "policy_id": policyID,
			"autonomy": cfg.autonomy, "description": "akili-loadtest simulated agent"}
		if err := api.retry(ctx, http.MethodPost, "/agents", body, &enr); err != nil {
			return nil, fmt.Errorf("create: %w", err)
		}
		ps.created.Add(1)
	}
	if enr.JoinToken == "" {
		return nil, errors.New("no join token in the response")
	}
	st, err := enroll(ctx, cfg, httpc, dir, enr.JoinToken, facts)
	if err != nil {
		return nil, err
	}
	return newSim(i, name, dir, st, cfg.url, facts)
}

// enroll mirrors `akili-agent enroll`: a fresh Ed25519 key is bound with the one-time join token.
func enroll(ctx context.Context, cfg *config, httpc *http.Client, dir, token string, facts proto.HostFacts) (*state.State, error) {
	work := filepath.Join(dir, "work")
	if err := os.MkdirAll(work, 0o750); err != nil {
		return nil, err
	}
	work, err := filepath.EvalSymlinks(work)
	if err != nil {
		return nil, err
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	facts.Workdir = work
	body, _ := json.Marshal(proto.EnrollRequest{JoinToken: token, PublicKey: pub, Facts: facts})
	delay := time.Second
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.url+proto.EnrollPath, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := httpc.Do(req)
		if err != nil {
			return nil, fmt.Errorf("enroll: %w", err)
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests && attempt < 12 {
			enrollThrottled.Add(1)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
			if delay *= 2; delay > 20*time.Second {
				delay = 20 * time.Second
			}
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("enroll rejected: %s: %s", resp.Status, bytes.TrimSpace(raw))
		}
		var er proto.EnrollResponse
		if err := json.Unmarshal(raw, &er); err != nil {
			return nil, fmt.Errorf("enroll: bad response: %w", err)
		}
		st := &state.State{URL: cfg.url, AgentID: er.AgentID, Name: er.Name, PrivateKey: priv, CPSigningKey: er.CPSigningKey,
			Workdir: work, CACert: cfg.caCert, Insecure: cfg.insecure}
		return st, state.Save(dir, st)
	}
}

var enrollThrottled atomic.Int64

func newSim(i int, name, dir string, st *state.State, url string, facts proto.HostFacts) (*sim, error) {
	st.URL = url
	facts.Workdir = st.Workdir
	a, err := link.New(st, "loadtest", func() proto.HostFacts { return facts })
	if err != nil {
		return nil, err
	}
	return &sim{idx: i, name: name, dir: dir, st: st, agent: a}, nil
}

// run keeps one simulated agent connected until ctx ends, with the same backoff as link.Run, and
// reports every connect, failure and loss to m.
func (s *sim) run(ctx context.Context, dialer *websocket.Dialer, m *metrics) {
	backoff := time.Second
	first := true
	for ctx.Err() == nil {
		t0 := time.Now()
		sess, status, err := s.agent.DialOnce(ctx, dialer)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			m.connectFailed(status, err, first)
		} else {
			m.connected(time.Since(t0), first)
			first = false
			backoff = time.Second
			err = s.agent.Serve(ctx, sess)
			_ = sess.Close()
			m.online.Add(-1)
			if ctx.Err() != nil {
				return
			}
			m.lost(s.name, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff + jitter(backoff)):
		}
		if backoff *= 2; backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
}

func jitter(d time.Duration) time.Duration {
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(d)/4+1))
	return time.Duration(n.Int64())
}
