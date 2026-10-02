// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"time"
)

// apiClient talks to the control plane's public API as an operator: an API key when one is given,
// otherwise a session cookie from logging in.
type apiClient struct {
	base   string
	key    string
	client *http.Client
}

// apiError is a non-2xx response.
type apiError struct {
	Status int
	Body   string
}

func (e *apiError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.Status, e.Body) }

func statusOf(err error) int {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae.Status
	}
	return 0
}

func newAPIClient(base, key string, tr http.RoundTripper) *apiClient {
	jar, _ := cookiejar.New(nil)
	return &apiClient{base: strings.TrimRight(base, "/"), key: key,
		client: &http.Client{Timeout: 60 * time.Second, Transport: tr, Jar: jar}}
}

// call sends a JSON request to /api/v1<path> and decodes the envelope's data into out.
func (c *apiClient) call(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+"/api/v1"+path, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 300 {
			msg = msg[:300] + "..."
		}
		return &apiError{Status: resp.StatusCode, Body: msg}
	}
	if out == nil {
		return nil
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("%s %s: bad response: %w", method, path, err)
	}
	return json.Unmarshal(env.Data, out)
}

// retry repeats call while the control plane answers 429 or 5xx, so a throttled prepare phase
// slows down instead of failing.
func (c *apiClient) retry(ctx context.Context, method, path string, body, out any) error {
	delay := time.Second
	for attempt := 0; ; attempt++ {
		err := c.call(ctx, method, path, body, out)
		s := statusOf(err)
		if err == nil || attempt >= 8 || (s != http.StatusTooManyRequests && s < 500 && s != 0) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		if delay *= 2; delay > 15*time.Second {
			delay = 15 * time.Second
		}
	}
}

func (c *apiClient) login(ctx context.Context, email, password string) error {
	return c.call(ctx, http.MethodPost, "/auth/login", map[string]string{"email": email, "password": password}, nil)
}

type apiAgent struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	PolicyID string `json:"policy_id"`
	Autonomy int    `json:"autonomy"`
}

type apiEnrollment struct {
	Agent     apiAgent `json:"agent"`
	JoinToken string   `json:"join_token"`
}

type apiTask struct {
	ID              string     `json:"id"`
	Status          string     `json:"status"`
	StatusReason    string     `json:"status_reason"`
	Error           string     `json:"error"`
	AssignedAgentID *string    `json:"assigned_agent_id"`
	CreatedAt       time.Time  `json:"created_at"`
	StartedAt       *time.Time `json:"started_at"`
	FinishedAt      *time.Time `json:"finished_at"`
}

func (c *apiClient) policyID(ctx context.Context, name string) (string, error) {
	var ps []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := c.retry(ctx, http.MethodGet, "/policies", nil, &ps); err != nil {
		return "", err
	}
	for _, p := range ps {
		if p.Name == name {
			return p.ID, nil
		}
	}
	return "", fmt.Errorf("policy %q not found", name)
}

// agents lists every agent in the organization. The endpoint has no paging: one call returns all.
func (c *apiClient) agents(ctx context.Context) ([]apiAgent, time.Duration, error) {
	var out []apiAgent
	t0 := time.Now()
	err := c.call(ctx, http.MethodGet, "/agents", nil, &out)
	return out, time.Since(t0), err
}
