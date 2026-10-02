// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package miabi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/audit"
	"github.com/goakili/akili/server/internal/models"
)

// JobTimeout bounds how long cron and pipeline runs are awaited.
var JobTimeout = 20 * time.Minute

func (s *Service) recordOp(ctx context.Context, sess *models.ChatSession, action string, t *target, meta map[string]any) {
	if meta == nil {
		meta = map[string]any{}
	}
	meta["workspace"], meta["session_id"] = t.ws.Name, sess.ID
	s.audit.Best(ctx, audit.Entry{OrganizationID: sess.OrganizationID, ActorType: audit.ActorAgent, ActorID: sess.AgentID, Action: action,
		TargetType: "integration", TargetID: t.it.ID, Metadata: meta})
}

// runWorkspaceOp runs the tools that do not address an app. handled is false for app tools.
func (s *Service) runWorkspaceOp(ctx context.Context, sess *models.ChatSession, t *target, c *Client, tool string, input json.RawMessage) (string, bool, error) {
	switch tool {
	case proto.ToolMiabiOverview:
		out, err := overview(ctx, c, t.ws.Name)
		return out, true, err
	case proto.ToolMiabiAlerts:
		var in proto.MiabiWorkspaceInput
		_ = json.Unmarshal(input, &in)
		as, err := c.Alerts(ctx, !in.All)
		if err != nil {
			return "", true, err
		}
		if len(as) == 0 {
			return "no alerts", true, nil
		}
		var b strings.Builder
		for _, a := range as {
			fmt.Fprintf(&b, "alert %d [%s/%s] %s: %s (%s %s, seen %d×, last %s)\n", a.ID, a.Severity, a.State, a.Category, a.Title, a.SubjectType, a.SubjectRef, a.Count, a.LastSeen.Format(time.RFC3339))
			if a.Body != "" {
				fmt.Fprintf(&b, "    %s\n", strings.TrimSpace(a.Body))
			}
		}
		return b.String(), true, nil
	case proto.ToolMiabiAlert:
		var in proto.MiabiAlertInput
		_ = json.Unmarshal(input, &in)
		a, err := c.AlertAction(ctx, in.Alert, in.Action)
		if err != nil {
			return "", true, err
		}
		s.recordOp(ctx, sess, "miabi.alert_"+in.Action, t, map[string]any{"alert_id": in.Alert})
		return fmt.Sprintf("alert %d is now %s", a.ID, a.State), true, nil
	case proto.ToolMiabiEvents:
		var in proto.MiabiWorkspaceInput
		_ = json.Unmarshal(input, &in)
		limit := in.Limit
		if limit <= 0 || limit > 100 {
			limit = 30
		}
		evs, err := c.RecentEvents(ctx, limit)
		if err != nil {
			return "", true, err
		}
		var b strings.Builder
		for i := len(evs) - 1; i >= 0; i-- {
			e := evs[i]
			subject := e.AppName
			if e.DatabaseName != "" {
				subject = "db " + e.DatabaseName
			}
			fmt.Fprintf(&b, "%s %-22s %-8s %s: %s\n", e.CreatedAt.Format(time.RFC3339), e.Type, e.Severity, subject, e.Message)
		}
		if b.Len() == 0 {
			return "no events", true, nil
		}
		return b.String(), true, nil
	case proto.ToolMiabiDatabases:
		dbs, err := c.Databases(ctx)
		if err != nil {
			return "", true, err
		}
		if len(dbs) == 0 {
			return "no databases", true, nil
		}
		var b strings.Builder
		for _, d := range dbs {
			health := ""
			if st, err := c.DatabaseStatus(ctx, d.ID); err == nil && st.Health != "" {
				health = ", health " + st.Health
			}
			fmt.Fprintf(&b, "- %s: %s %s, %s%s, %d MB\n", d.Name, d.Engine, d.Version, d.Status, health, d.SizeBytes>>20)
		}
		return b.String(), true, nil
	case proto.ToolMiabiDBBackups, proto.ToolMiabiDBBackup, proto.ToolMiabiDBRestore:
		out, err := s.database(ctx, sess, t, c, tool, input)
		return out, true, err
	case proto.ToolMiabiStackRestart:
		var in proto.MiabiNamedInput
		_ = json.Unmarshal(input, &in)
		st, err := c.StackByName(ctx, in.Name)
		if err != nil {
			return "", true, err
		}
		rs, err := c.RestartStack(ctx, st.ID)
		if err != nil {
			return "", true, err
		}
		s.recordOp(ctx, sess, "miabi.stack_restart", t, map[string]any{"stack": st.Name})
		var b strings.Builder
		failed := 0
		for _, r := range rs {
			fmt.Fprintf(&b, "- %s: %s %s\n", r.AppName, r.Status, r.Error)
			if r.Status == "failed" {
				failed++
			}
		}
		if failed > 0 {
			return b.String(), true, fmt.Errorf("%d app(s) failed to restart", failed)
		}
		return b.String(), true, nil
	case proto.ToolMiabiCronJobs:
		var in proto.MiabiWorkspaceInput
		_ = json.Unmarshal(input, &in)
		cs, err := c.CronJobs(ctx)
		if err != nil {
			return "", true, err
		}
		var b strings.Builder
		for _, j := range cs {
			if in.App != "" && !strings.EqualFold(j.AppName, in.App) {
				continue
			}
			last := "never"
			if j.LastRunAt != nil {
				last = j.LastRunAt.Format(time.RFC3339)
			}
			fmt.Fprintf(&b, "- %s (app %s): %q enabled=%v last run %s\n", j.Name, j.AppName, j.Schedule, j.Enabled, last)
		}
		if b.Len() == 0 {
			return "no cron jobs", true, nil
		}
		return b.String(), true, nil
	case proto.ToolMiabiCronRun:
		var in proto.MiabiNamedInput
		_ = json.Unmarshal(input, &in)
		cs, err := c.CronJobs(ctx)
		if err != nil {
			return "", true, err
		}
		var job *CronJob
		for i := range cs {
			if strings.EqualFold(cs[i].Name, in.Name) {
				job = &cs[i]
			}
		}
		if job == nil {
			return "", true, fmt.Errorf("no cron job named %q in workspace %s", in.Name, t.ws.Name)
		}
		run, err := c.RunCronJob(ctx, job.ID)
		if err != nil {
			return "", true, err
		}
		s.recordOp(ctx, sess, "miabi.cron_run", t, map[string]any{"cronjob": job.Name, "job_id": run.ID})
		run, err = waitStatus(ctx, c.Poll, JobTimeout, run, func() (*Job, error) { return c.GetJob(ctx, run.ID) }, func(j *Job) string { return j.Status })
		if err != nil {
			return "", true, err
		}
		out := fmt.Sprintf("cron job %s: run %d %s", job.Name, run.ID, run.Status)
		if run.Error != "" {
			out += "\nerror: " + run.Error
		}
		if run.Status != "succeeded" {
			return out, true, errors.New("the run did not succeed")
		}
		return out, true, nil
	case proto.ToolMiabiPipelines:
		ps, err := c.Pipelines(ctx)
		if err != nil {
			return "", true, err
		}
		var b strings.Builder
		for _, p := range ps {
			last := "never run"
			if p.LastRun != nil {
				last = fmt.Sprintf("last run #%d %s (%s)", p.LastRun.Number, p.LastRun.Status, p.LastRun.Branch)
			}
			fmt.Fprintf(&b, "- %s (branch %s, enabled=%v): %s\n", p.Name, p.Branch, p.Enabled, last)
		}
		if b.Len() == 0 {
			return "no pipelines", true, nil
		}
		return b.String(), true, nil
	case proto.ToolMiabiPipelineRun:
		var in proto.MiabiNamedInput
		_ = json.Unmarshal(input, &in)
		ps, err := c.Pipelines(ctx)
		if err != nil {
			return "", true, err
		}
		var pl *Pipeline
		for i := range ps {
			if strings.EqualFold(ps[i].Name, in.Name) {
				pl = &ps[i]
			}
		}
		if pl == nil {
			return "", true, fmt.Errorf("no pipeline named %q in workspace %s", in.Name, t.ws.Name)
		}
		run, err := c.TriggerPipeline(ctx, pl.ID, in.Branch)
		if err != nil {
			return "", true, err
		}
		s.recordOp(ctx, sess, "miabi.pipeline_run", t, map[string]any{"pipeline": pl.Name, "run_id": run.ID, "branch": in.Branch})
		run, err = waitStatus(ctx, c.Poll, JobTimeout, run, func() (*PipelineRun, error) { return c.GetPipelineRun(ctx, run.ID) }, func(r *PipelineRun) string { return r.Status })
		if err != nil {
			return "", true, err
		}
		out := fmt.Sprintf("pipeline %s: run #%d %s (branch %s, commit %s)", pl.Name, run.Number, run.Status, run.Branch, run.Commit)
		if run.Error != "" {
			out += "\nerror: " + run.Error
		}
		if run.Status != "succeeded" {
			return out, true, errors.New("the run did not succeed")
		}
		return out, true, nil
	}
	return "", false, nil
}

// waitStatus polls until a run reaches a terminal status.
func waitStatus[T any](ctx context.Context, poll, timeout time.Duration, cur *T, get func() (*T, error), status func(*T) string) (*T, error) {
	deadline := time.Now().Add(timeout)
	for {
		switch status(cur) {
		case "succeeded", "failed", "canceled", "completed":
			return cur, nil
		}
		if time.Now().After(deadline) {
			return cur, fmt.Errorf("still %s after %s", status(cur), timeout)
		}
		select {
		case <-ctx.Done():
			return cur, ctx.Err()
		case <-time.After(poll):
		}
		next, err := get()
		if err != nil {
			return cur, err
		}
		cur = next
	}
}

func overview(ctx context.Context, c *Client, ws string) (string, error) {
	o, err := c.Overview(ctx)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "workspace %s: %d apps (%d running, %d failed), %d databases, %d stacks\n", ws, o.TotalApps, o.Running, o.Failed, o.Databases, o.Stacks)
	for _, a := range o.Apps {
		fmt.Fprintf(&b, "- %s: %s, health %s\n", a.Name, a.Status, a.Health)
	}
	if u, err := c.Usage(ctx); err == nil {
		q := func(name string, v Quota) string {
			if v.Limit < 0 {
				return fmt.Sprintf("%s %d (unlimited)", name, v.Used)
			}
			return fmt.Sprintf("%s %d/%d", name, v.Used, v.Limit)
		}
		fmt.Fprintf(&b, "plan %s: %s, %s, %s, %s\n", u.PlanName, q("apps", u.Apps), q("databases", u.DatabaseInstances), q("cpu", u.CPUCores), q("memory MB", u.MemoryMB))
	}
	if len(o.RecentEvents) > 0 {
		b.WriteString("recent events:\n")
		for _, e := range o.RecentEvents {
			fmt.Fprintf(&b, "  %s %s %s: %s\n", e.CreatedAt.Format(time.RFC3339), e.Type, e.AppName, e.Message)
		}
	}
	return b.String(), nil
}

// database runs the backup tools. Instances and logical databases are addressed by name only.
func (s *Service) database(ctx context.Context, sess *models.ChatSession, t *target, c *Client, tool string, input json.RawMessage) (string, error) {
	var in proto.MiabiDatabaseInput
	_ = json.Unmarshal(input, &in)
	inst, err := c.DatabaseByName(ctx, in.Database)
	if err != nil {
		return "", err
	}
	dbs, err := c.LogicalDatabases(ctx, inst.ID)
	if err != nil {
		return "", err
	}
	var db *LogicalDatabase
	switch {
	case in.DB != "":
		for i := range dbs {
			if strings.EqualFold(dbs[i].Name, in.DB) {
				db = &dbs[i]
			}
		}
		if db == nil {
			return "", fmt.Errorf("database %s has no database named %q", inst.Name, in.DB)
		}
	case len(dbs) == 1:
		db = &dbs[0]
	default:
		names := make([]string, len(dbs))
		for i := range dbs {
			names[i] = dbs[i].Name
		}
		return "", fmt.Errorf("database %s holds several databases (%s); pass \"db\"", inst.Name, strings.Join(names, ", "))
	}
	switch tool {
	case proto.ToolMiabiDBBackups:
		bs, err := c.Backups(ctx, inst.ID, db.ID)
		if err != nil {
			return "", err
		}
		if len(bs) == 0 {
			return "no backups of " + inst.Name + "/" + db.Name, nil
		}
		var b strings.Builder
		for _, bk := range bs {
			fmt.Fprintf(&b, "backup %d: %s, %s to %s, %d KB, %s", bk.Number, bk.Status, bk.Trigger, bk.Destination, bk.SizeBytes>>10, bk.CreatedAt.Format(time.RFC3339))
			if bk.Error != "" {
				b.WriteString(" error: " + bk.Error)
			}
			b.WriteString("\n")
		}
		return b.String(), nil
	case proto.ToolMiabiDBBackup:
		bk, err := c.CreateBackup(ctx, inst.ID, db.ID, in.Comment)
		if err != nil {
			return "", err
		}
		s.recordOp(ctx, sess, "miabi.db_backup", t, map[string]any{"database": inst.Name, "db": db.Name, "backup": bk.Number, "status": bk.Status})
		out := fmt.Sprintf("backup %d of %s/%s: %s, %d KB", bk.Number, inst.Name, db.Name, bk.Status, bk.SizeBytes>>10)
		if bk.Status == "failed" {
			return out + "\nerror: " + bk.Error, errors.New("the backup failed")
		}
		return out, nil
	case proto.ToolMiabiDBRestore:
		bs, err := c.Backups(ctx, inst.ID, db.ID)
		if err != nil {
			return "", err
		}
		var bk *Backup
		for i := range bs {
			if int64(bs[i].Number) == in.Backup {
				bk = &bs[i]
			}
		}
		if bk == nil {
			return "", fmt.Errorf("%s/%s has no backup %d", inst.Name, db.Name, in.Backup)
		}
		if bk.Status != "completed" {
			return "", fmt.Errorf("backup %d is %s, not completed", bk.Number, bk.Status)
		}
		// Recorded before acting: a restore overwrites data, so its intent must be on the trail even
		// if the call never returns.
		s.recordOp(ctx, sess, "miabi.db_restore", t, map[string]any{"database": inst.Name, "db": db.Name, "backup": bk.Number})
		if err := c.RestoreBackup(ctx, inst.ID, db.ID, bk.ID); err != nil {
			return "", err
		}
		return fmt.Sprintf("restored %s/%s from backup %d (%s)", inst.Name, db.Name, bk.Number, bk.CreatedAt.Format(time.RFC3339)), nil
	}
	return "", fmt.Errorf("%s is not a database tool", tool)
}

// runAppOp runs the app tools added beyond deploy/rollback/restart. handled is false for others.
func (s *Service) runAppOp(ctx context.Context, sess *models.ChatSession, t *target, c *Client, app *App, tool string, input json.RawMessage) (string, bool, error) {
	meta := func(m map[string]any) map[string]any {
		m["app"], m["app_id"] = app.Name, app.ID
		return m
	}
	switch tool {
	case proto.ToolMiabiTraffic:
		var in proto.MiabiTrafficInput
		_ = json.Unmarshal(input, &in)
		out, err := traffic(ctx, c, app, in)
		return out, true, err
	case proto.ToolMiabiEnv:
		vs, err := c.Env(ctx, app.ID)
		if err != nil {
			return "", true, err
		}
		var b strings.Builder
		for _, v := range vs {
			if v.IsSecret {
				fmt.Fprintf(&b, "%s=<secret>\n", v.Key)
			} else {
				fmt.Fprintf(&b, "%s=%s\n", v.Key, v.Value)
			}
		}
		if b.Len() == 0 {
			return "no environment variables", true, nil
		}
		return b.String(), true, nil
	case proto.ToolMiabiEnvSet:
		var in proto.MiabiEnvSetInput
		_ = json.Unmarshal(input, &in)
		vs, err := c.Env(ctx, app.ID)
		if err != nil {
			return "", true, err
		}
		for _, v := range vs {
			if v.Key == in.Key && v.IsSecret {
				return "", true, fmt.Errorf("%s is a secret; secrets are changed in Miabi, not by agents", in.Key)
			}
		}
		msg, err := c.SetEnv(ctx, app.ID, in.Key, in.Value)
		if err != nil {
			return "", true, err
		}
		s.recordOp(ctx, sess, "miabi.env_set", t, meta(map[string]any{"key": in.Key}))
		return fmt.Sprintf("%s set on %s: %s (deploy the app for it to take effect)", in.Key, app.Name, msg), true, nil
	case proto.ToolMiabiScale:
		var in proto.MiabiScaleInput
		_ = json.Unmarshal(input, &in)
		if err := c.Scale(ctx, app.ID, in.Replicas); err != nil {
			return "", true, err
		}
		s.recordOp(ctx, sess, "miabi.scale", t, meta(map[string]any{"replicas": in.Replicas}))
		time.Sleep(2 * time.Second)
		st, _ := s.status(ctx, c, app)
		return fmt.Sprintf("scaled %s to %d replicas\n\n%s", app.Name, in.Replicas, st), true, nil
	case proto.ToolMiabiMaintenance:
		var in proto.MiabiMaintenanceInput
		_ = json.Unmarshal(input, &in)
		rs, err := c.AppRoutes(ctx, app.ID)
		if err != nil {
			return "", true, err
		}
		if len(rs) == 0 {
			return "", true, fmt.Errorf("%s has no routes", app.Name)
		}
		for _, r := range rs {
			if err := c.SetMaintenance(ctx, r.ID, in.Enabled, in.Message); err != nil {
				return "", true, fmt.Errorf("route %s: %w", r.Name, err)
			}
		}
		s.recordOp(ctx, sess, "miabi.maintenance", t, meta(map[string]any{"enabled": in.Enabled, "routes": len(rs)}))
		state := "off"
		if in.Enabled {
			state = "on"
		}
		return fmt.Sprintf("maintenance %s for %d route(s) of %s\nmaintenance: %s", state, len(rs), app.Name, state), true, nil
	case proto.ToolMiabiCanary:
		var in proto.MiabiCanaryInput
		_ = json.Unmarshal(input, &in)
		if in.Action == "abort" {
			if err := c.CanaryAbort(ctx, app.ID); err != nil {
				return "", true, err
			}
			s.recordOp(ctx, sess, "miabi.canary_abort", t, meta(map[string]any{}))
			st, _ := s.status(ctx, c, app)
			return "canary aborted\n\n" + st, true, nil
		}
		d, err := c.CanaryPromote(ctx, app.ID)
		if err != nil {
			return "", true, err
		}
		s.recordOp(ctx, sess, "miabi.canary_promote", t, meta(map[string]any{"deployment_id": d.ID}))
		out, err := s.finish(ctx, c, app, d, "canary promotion")
		return out, true, err
	}
	return "", false, nil
}

// traffic reports an app's HTTP health with a verdict line for change_run checks.
func traffic(ctx context.Context, c *Client, app *App, in proto.MiabiTrafficInput) (string, error) {
	window := in.Range
	switch window {
	case "", "15m", "1h", "24h":
	default:
		return "", fmt.Errorf("range must be 15m, 1h or 24h")
	}
	if window == "" {
		window = "15m"
	}
	maxErr := in.MaxErrorRate
	if maxErr <= 0 {
		maxErr = 0.02
	}
	t, err := c.Traffic(ctx, app.ID, window)
	if err != nil {
		return "", err
	}
	verdict, why := "ok", ""
	switch {
	case t.Requests == 0:
		verdict, why = "no-data", "no requests in the window"
	case t.ErrorRate > maxErr:
		verdict, why = "degraded", fmt.Sprintf("error rate %.2f%% above %.2f%%", t.ErrorRate*100, maxErr*100)
	case in.MaxP95Ms > 0 && t.P95Latency > float64(in.MaxP95Ms):
		verdict, why = "degraded", fmt.Sprintf("p95 %.0f ms above %d ms", t.P95Latency, in.MaxP95Ms)
	}
	out := fmt.Sprintf("app: %s\nwindow: %s\nrequests: %d\nerror_rate: %.2f%%\np95_ms: %.0f\np99_ms: %.0f\ntraffic: %s", app.Name, window, t.Requests,
		t.ErrorRate*100, t.P95Latency, t.P99Latency, verdict)
	if why != "" {
		out += " (" + why + ")"
	}
	return out, nil
}
