// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package miabi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Overview is a workspace's dashboard summary.
type Overview struct {
	Apps []struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
		Health string `json:"health"`
	} `json:"apps"`
	TotalApps    int     `json:"total_apps"`
	Running      int     `json:"running"`
	Failed       int     `json:"failed"`
	Databases    int     `json:"databases"`
	Stacks       int     `json:"stacks"`
	RecentEvents []Event `json:"recent_events"`
}

// Overview reads the workspace summary.
func (c *Client) Overview(ctx context.Context) (*Overview, error) {
	var o Overview
	return &o, c.do(ctx, http.MethodGet, c.ws()+"/overview", nil, &o)
}

// Quota is one usage counter (-1 limit: unlimited).
type Quota struct {
	Used  int64 `json:"used"`
	Limit int   `json:"limit"`
}

// Usage is the workspace's plan and quota use.
type Usage struct {
	PlanName          string `json:"plan_name"`
	Apps              Quota  `json:"apps"`
	DatabaseInstances Quota  `json:"database_instances"`
	CPUCores          Quota  `json:"cpu_cores"`
	MemoryMB          Quota  `json:"memory_mb"`
	StorageMB         Quota  `json:"storage_mb"`
}

// Usage reads quota use.
func (c *Client) Usage(ctx context.Context) (*Usage, error) {
	var u Usage
	return &u, c.do(ctx, http.MethodGet, c.ws()+"/usage", nil, &u)
}

// Alert is a Miabi alert.
type Alert struct {
	ID          int64     `json:"id"`
	Category    string    `json:"category"`
	Severity    string    `json:"severity"`
	State       string    `json:"state"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	Count       int       `json:"count"`
	SubjectType string    `json:"subject_type"`
	SubjectRef  string    `json:"subject_ref"`
	LastSeen    time.Time `json:"last_seen"`
}

// Alerts lists alerts; active limits them to firing and acknowledged ones.
func (c *Client) Alerts(ctx context.Context, active bool) ([]Alert, error) {
	var as []Alert
	q := ""
	if active {
		q = "?active=true"
	}
	return as, c.do(ctx, http.MethodGet, c.ws()+"/alerts"+q, nil, &as)
}

// AlertAction acknowledges or resolves an alert ("ack" | "resolve").
func (c *Client) AlertAction(ctx context.Context, id int64, action string) (*Alert, error) {
	var a Alert
	return &a, c.do(ctx, http.MethodPost, fmt.Sprintf("%s/alerts/%d/%s", c.ws(), id, action), map[string]any{}, &a)
}

// DatabaseInstance is a managed database server.
type DatabaseInstance struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Engine      string `json:"engine"`
	Version     string `json:"version"`
	Status      string `json:"status"`
	SizeBytes   int64  `json:"size_bytes"`
}

// LogicalDatabase is one database on an instance.
type LogicalDatabase struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// Backup is one database backup.
type Backup struct {
	ID          int64      `json:"id"`
	Number      int        `json:"number"`
	Status      string     `json:"status"` // pending | running | completed | failed
	Trigger     string     `json:"trigger"`
	Destination string     `json:"destination"`
	SizeBytes   int64      `json:"size_bytes"`
	Comment     string     `json:"comment"`
	Error       string     `json:"error"`
	CreatedAt   time.Time  `json:"created_at"`
	FinishedAt  *time.Time `json:"finished_at"`
}

// Databases lists the database instances.
func (c *Client) Databases(ctx context.Context) ([]DatabaseInstance, error) {
	var ds []DatabaseInstance
	return ds, c.do(ctx, http.MethodGet, c.ws()+"/databases", nil, &ds)
}

// DatabaseByName finds an instance by its name (slug) only.
func (c *Client) DatabaseByName(ctx context.Context, name string) (*DatabaseInstance, error) {
	ds, err := c.Databases(ctx)
	if err != nil {
		return nil, err
	}
	for i := range ds {
		if strings.EqualFold(ds[i].Name, name) {
			return &ds[i], nil
		}
	}
	return nil, fmt.Errorf("%w: no Miabi database named %q in workspace %s", ErrNotFound, name, c.workspace)
}

// DatabaseStatus reads an instance's live status.
func (c *Client) DatabaseStatus(ctx context.Context, id int64) (*LiveStatus, error) {
	var st LiveStatus
	return &st, c.do(ctx, http.MethodGet, fmt.Sprintf("%s/databases/%d/status", c.ws(), id), nil, &st)
}

// LogicalDatabases lists the databases on an instance.
func (c *Client) LogicalDatabases(ctx context.Context, id int64) ([]LogicalDatabase, error) {
	var ls []LogicalDatabase
	return ls, c.do(ctx, http.MethodGet, fmt.Sprintf("%s/databases/%d/databases", c.ws(), id), nil, &ls)
}

// Backups lists a logical database's backups.
func (c *Client) Backups(ctx context.Context, instance, db int64) ([]Backup, error) {
	var bs []Backup
	return bs, c.do(ctx, http.MethodGet, fmt.Sprintf("%s/databases/%d/databases/%d/backups", c.ws(), instance, db), nil, &bs)
}

// CreateBackup takes a backup now (Miabi runs it synchronously).
func (c *Client) CreateBackup(ctx context.Context, instance, db int64, comment string) (*Backup, error) {
	var b Backup
	return &b, c.do(ctx, http.MethodPost, fmt.Sprintf("%s/databases/%d/databases/%d/backups", c.ws(), instance, db), map[string]any{"comment": comment}, &b)
}

// RestoreBackup restores a backup (Miabi blocks until it finishes). Never "force": that drops the
// database first.
func (c *Client) RestoreBackup(ctx context.Context, instance, db, backup int64) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("%s/databases/%d/databases/%d/backups/%d/restore", c.ws(), instance, db, backup),
		map[string]any{"method": "normal"}, nil)
}

// Traffic is an app's HTTP traffic over a window. ErrorRate is a fraction (0-1).
type Traffic struct {
	Requests    int64   `json:"requests"`
	ErrorRate   float64 `json:"error_rate"`
	P95Latency  float64 `json:"p95_latency_ms"`
	P99Latency  float64 `json:"p99_latency_ms"`
	AvgLatency  float64 `json:"avg_latency_ms"`
	Status5xx   int64   `json:"-"`
	WindowLabel string  `json:"-"`
}

// Traffic reads an app's traffic summary for a window ("15m", "1h", "24h").
func (c *Client) Traffic(ctx context.Context, appID int64, window string) (*Traffic, error) {
	var out struct {
		Totals Traffic `json:"totals"`
		Status struct {
			S5xx int64 `json:"s5xx"`
		} `json:"status"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("%s/analytics/summary?range=%s&app=%d", c.ws(), url.QueryEscape(window), appID), nil, &out); err != nil {
		return nil, err
	}
	t := out.Totals
	t.Status5xx, t.WindowLabel = out.Status.S5xx, window
	return &t, nil
}

// EnvVar is an app environment variable; secret values come back masked.
type EnvVar struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	IsSecret bool   `json:"is_secret"`
}

// Env lists an app's variables.
func (c *Client) Env(ctx context.Context, appID int64) ([]EnvVar, error) {
	var vs []EnvVar
	return vs, c.do(ctx, http.MethodGet, fmt.Sprintf("%s/env", c.app(appID)), nil, &vs)
}

// SetEnv upserts one non-secret variable and returns Miabi's message (it says when a redeploy is needed).
func (c *Client) SetEnv(ctx context.Context, appID int64, key, value string) (string, error) {
	var m struct {
		Message string `json:"message"`
	}
	err := c.do(ctx, http.MethodPut, fmt.Sprintf("%s/env", c.app(appID)), map[string]any{"key": key, "value": value, "is_secret": false}, &m)
	return m.Message, err
}

// Scale sets an app's replicas.
func (c *Client) Scale(ctx context.Context, appID int64, replicas int) error {
	return c.do(ctx, http.MethodPost, c.app(appID)+"/scale", map[string]any{"replicas": replicas}, nil)
}

// RouteRef is an app's route with its maintenance state.
type RouteRef struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	Hosts       []string `json:"hosts"`
	Maintenance struct {
		Enabled bool `json:"enabled"`
	} `json:"maintenance"`
}

// AppRoutes lists an app's routes.
func (c *Client) AppRoutes(ctx context.Context, appID int64) ([]RouteRef, error) {
	var rs []RouteRef
	return rs, c.do(ctx, http.MethodGet, c.ws()+"/routes?application_id="+strconv.FormatInt(appID, 10), nil, &rs)
}

// SetMaintenance turns maintenance mode on or off for one route.
func (c *Client) SetMaintenance(ctx context.Context, routeID int64, enabled bool, message string) error {
	return c.do(ctx, http.MethodPatch, fmt.Sprintf("%s/routes/%d/maintenance", c.ws(), routeID),
		map[string]any{"enabled": enabled, "message": message}, nil)
}

// CanaryPromote sends all traffic to the canary (a new rolling deployment).
func (c *Client) CanaryPromote(ctx context.Context, appID int64) (*Deployment, error) {
	var d Deployment
	return &d, c.do(ctx, http.MethodPost, c.app(appID)+"/canary/promote", map[string]any{}, &d)
}

// CanaryAbort drops the canary.
func (c *Client) CanaryAbort(ctx context.Context, appID int64) error {
	return c.do(ctx, http.MethodDelete, c.app(appID)+"/canary", nil, nil)
}

// Stack is a group of apps.
type Stack struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	AppCount int64  `json:"app_count"`
	Status   struct {
		Total   int `json:"total"`
		Running int `json:"running"`
		Failed  int `json:"failed"`
	} `json:"status"`
}

// StackByName finds a stack by name only.
func (c *Client) StackByName(ctx context.Context, name string) (*Stack, error) {
	var ss []Stack
	if err := c.do(ctx, http.MethodGet, c.ws()+"/stacks", nil, &ss); err != nil {
		return nil, err
	}
	for i := range ss {
		if strings.EqualFold(ss[i].Name, name) {
			return &ss[i], nil
		}
	}
	return nil, fmt.Errorf("%w: no Miabi stack named %q in workspace %s", ErrNotFound, name, c.workspace)
}

// StackRestartResult is one app's outcome.
type StackRestartResult struct {
	AppName string `json:"app_name"`
	Status  string `json:"status"` // ok | skipped | failed
	Error   string `json:"error"`
}

// RestartStack restarts a stack's apps one at a time.
func (c *Client) RestartStack(ctx context.Context, id int64) ([]StackRestartResult, error) {
	var rs []StackRestartResult
	return rs, c.do(ctx, http.MethodPost, fmt.Sprintf("%s/stacks/%d/restart?rolling=true", c.ws(), id), map[string]any{}, &rs)
}

// CronJob is a scheduled job.
type CronJob struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	AppName   string     `json:"app_name"`
	Schedule  string     `json:"schedule"`
	Enabled   bool       `json:"enabled"`
	LastRunAt *time.Time `json:"last_run_at"`
}

// Job is one run of a job.
type Job struct {
	ID       int64  `json:"id"`
	Status   string `json:"status"` // pending | running | succeeded | failed | canceled
	ExitCode *int   `json:"exit_code"`
	Error    string `json:"error"`
}

// CronJobs lists cron jobs.
func (c *Client) CronJobs(ctx context.Context) ([]CronJob, error) {
	var cs []CronJob
	return cs, c.do(ctx, http.MethodGet, c.ws()+"/cronjobs", nil, &cs)
}

// RunCronJob starts a cron job now.
func (c *Client) RunCronJob(ctx context.Context, id int64) (*Job, error) {
	var j Job
	return &j, c.do(ctx, http.MethodPost, fmt.Sprintf("%s/cronjobs/%d/run", c.ws(), id), map[string]any{}, &j)
}

// GetJob reads a job.
func (c *Client) GetJob(ctx context.Context, id int64) (*Job, error) {
	var j Job
	return &j, c.do(ctx, http.MethodGet, fmt.Sprintf("%s/jobs/%d", c.ws(), id), nil, &j)
}

// PipelineRun is one pipeline run.
type PipelineRun struct {
	ID     int64  `json:"id"`
	Number int    `json:"number"`
	Status string `json:"status"` // pending | running | succeeded | failed | canceled
	Branch string `json:"branch"`
	Commit string `json:"commit"`
	Error  string `json:"error"`
}

// Pipeline is a pipeline definition with its last run.
type Pipeline struct {
	ID      int64        `json:"id"`
	Name    string       `json:"name"`
	Branch  string       `json:"branch"`
	Enabled bool         `json:"enabled"`
	LastRun *PipelineRun `json:"last_run"`
}

// Pipelines lists pipelines (first page of 100).
func (c *Client) Pipelines(ctx context.Context) ([]Pipeline, error) {
	var ps []Pipeline
	return ps, c.do(ctx, http.MethodGet, c.ws()+"/pipelines?page=0&size=100", nil, &ps)
}

// TriggerPipeline starts a run (branch empty: the tracked ref).
func (c *Client) TriggerPipeline(ctx context.Context, id int64, branch string) (*PipelineRun, error) {
	var r PipelineRun
	return &r, c.do(ctx, http.MethodPost, fmt.Sprintf("%s/pipelines/%d/trigger", c.ws(), id), map[string]any{"branch": branch}, &r)
}

// GetPipelineRun reads a run.
func (c *Client) GetPipelineRun(ctx context.Context, id int64) (*PipelineRun, error) {
	var r PipelineRun
	return &r, c.do(ctx, http.MethodGet, fmt.Sprintf("%s/pipeline-runs/%d", c.ws(), id), nil, &r)
}
