// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package miabi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/audit"
	"github.com/goakili/akili/server/internal/crypto"
	"github.com/goakili/akili/server/internal/models"
	"gorm.io/gorm"
)

// Tools are the Miabi tools this service runs on the control plane.
var Tools = []string{proto.ToolMiabiWorkspaces, proto.ToolMiabiOverview, proto.ToolMiabiAlerts, proto.ToolMiabiAlert, proto.ToolMiabiEvents,
	proto.ToolMiabiDatabases, proto.ToolMiabiDBBackups, proto.ToolMiabiDBBackup, proto.ToolMiabiDBRestore, proto.ToolMiabiTraffic, proto.ToolMiabiEnv,
	proto.ToolMiabiEnvSet, proto.ToolMiabiScale, proto.ToolMiabiMaintenance, proto.ToolMiabiCanary, proto.ToolMiabiStackRestart, proto.ToolMiabiCronJobs,
	proto.ToolMiabiCronRun, proto.ToolMiabiPipelines, proto.ToolMiabiPipelineRun, proto.ToolMiabiApps, proto.ToolMiabiStatus, proto.ToolMiabiDeployments, proto.ToolMiabiReleases, proto.ToolMiabiLogs,
	proto.ToolMiabiDeployLogs, proto.ToolMiabiDeploy, proto.ToolMiabiRollback, proto.ToolMiabiRestart}

// DeployTimeout bounds how long a deploy or rollback tool waits for Miabi.
var DeployTimeout = 15 * time.Minute

// Service runs Miabi tools and turns Miabi webhooks into tasks.
type Service struct {
	db    *gorm.DB
	box   *crypto.Box
	audit *audit.Logger

	mu      sync.Mutex
	clients map[string]cachedClient
	streams map[string]bool // workspace row id → live stream open
}

type cachedClient struct {
	updated time.Time
	c       *Client
}

// NewService returns the Miabi service.
func NewService(db *gorm.DB, box *crypto.Box, a *audit.Logger) *Service {
	return &Service{db: db, box: box, audit: a, clients: map[string]cachedClient{}}
}

// ClientFor builds the client for a Miabi integration.
func (s *Service) ClientFor(it *models.Integration) (*Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.clients[it.ID]; ok && c.updated.Equal(it.UpdatedAt) {
		return c.c, nil
	}
	key, err := s.box.Decrypt(it.TokenEnc)
	if err != nil {
		return nil, err
	}
	c := New(it.BaseURL, it.Workspace, key)
	if it.CACert != "" {
		if err := c.TrustCA(it.CACert); err != nil {
			return nil, err
		}
	}
	s.clients[it.ID] = cachedClient{updated: it.UpdatedAt, c: c}
	return c, nil
}

// Test checks an integration's key and refreshes the workspaces it can reach.
func (s *Service) Test(ctx context.Context, it *models.Integration) (string, error) {
	c, err := s.ClientFor(it)
	if err != nil {
		return "", err
	}
	b, err := c.Binding(ctx)
	if err != nil {
		return "", err
	}
	rows, err := s.SyncWorkspaces(ctx, it)
	if err != nil {
		return "", fmt.Errorf("authenticated as %s, but listing workspaces failed: %w", b.User, err)
	}
	enabled, reachable := 0, 0
	for _, r := range rows {
		if r.Accessible {
			reachable++
		}
		if r.Enabled {
			enabled++
		}
	}
	scope := "account-wide key"
	if b.WorkspaceID != 0 {
		scope = "key bound to one workspace"
	}
	return fmt.Sprintf("%s · %s · %d workspaces reachable, %d enabled", b.User, scope, reachable, enabled), nil
}

// SyncWorkspaces records the workspaces the key can reach. New workspaces start disabled, except
// the integration's own workspace and the only reachable one, which are enabled so existing setups
// keep working. Watches that name a workspace by id or uid are rewritten to its handle.
func (s *Service) SyncWorkspaces(ctx context.Context, it *models.Integration) ([]models.MiabiWorkspace, error) {
	c, err := s.ClientFor(it)
	if err != nil {
		return nil, err
	}
	b, err := c.Binding(ctx)
	if err != nil {
		return nil, err
	}
	var list []Workspace
	if b.WorkspaceID != 0 {
		w, err := c.Workspace(ctx, strconv.FormatInt(b.WorkspaceID, 10))
		if err != nil {
			return nil, err
		}
		list = []Workspace{*w}
	} else if list, err = c.Workspaces(ctx); err != nil {
		return nil, err
	}
	reachable := 0
	for _, w := range list {
		if b.WorkspaceID == 0 || b.WorkspaceID == w.ID {
			reachable++
		}
	}
	now := time.Now().UTC()
	var out []models.MiabiWorkspace
	for _, w := range list {
		accessible := b.WorkspaceID == 0 || b.WorkspaceID == w.ID
		var row models.MiabiWorkspace
		err := s.db.WithContext(ctx).First(&row, "integration_id = ? AND name = ?", it.ID, w.Name).Error
		isNew := errors.Is(err, gorm.ErrRecordNotFound)
		if isNew {
			row = models.MiabiWorkspace{Base: models.Base{ID: models.NewID("mbs"), OrganizationID: it.OrganizationID}, IntegrationID: it.ID, Name: w.Name}
			named := it.Workspace != "" && (it.Workspace == w.Name || it.Workspace == strconv.FormatInt(w.ID, 10) || it.Workspace == w.UID)
			row.Enabled = accessible && (named || reachable == 1)
		} else if err != nil {
			return nil, err
		}
		row.MiabiID, row.DisplayName, row.Role, row.Accessible, row.SyncedAt = w.ID, w.DisplayName, w.Role, accessible, &now
		if !accessible {
			row.Enabled = false
		}
		if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
			return nil, err
		}
		for _, alias := range []string{strconv.FormatInt(w.ID, 10), w.UID} {
			if alias != "" && alias != w.Name {
				s.db.WithContext(ctx).Model(&models.MiabiWatch{}).Where("integration_id = ? AND workspace = ?", it.ID, alias).Update("workspace", w.Name)
			}
		}
		out = append(out, row)
	}
	// A workspace the key no longer reaches (bound key, membership removed) stays listed but unusable.
	seen := make([]string, 0, len(list))
	for _, w := range list {
		seen = append(seen, w.Name)
	}
	q := s.db.WithContext(ctx).Where("integration_id = ?", it.ID)
	if len(seen) > 0 {
		q = q.Where("name NOT IN ?", seen)
	}
	var stale []models.MiabiWorkspace
	if err := q.Find(&stale).Error; err != nil {
		return nil, err
	}
	for i := range stale {
		stale[i].Accessible, stale[i].Enabled, stale[i].SyncedAt = false, false, &now
		if err := s.db.WithContext(ctx).Save(&stale[i]).Error; err != nil {
			return nil, err
		}
		out = append(out, stale[i])
	}
	return out, nil
}

// workspace returns an enabled, reachable workspace of the integration by its handle, syncing once
// if none is known yet.
func (s *Service) workspace(ctx context.Context, it *models.Integration, name string) (*models.MiabiWorkspace, error) {
	var row models.MiabiWorkspace
	err := s.db.WithContext(ctx).First(&row, "integration_id = ? AND name = ?", it.ID, name).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		var n int64
		s.db.WithContext(ctx).Model(&models.MiabiWorkspace{}).Where("integration_id = ?", it.ID).Count(&n)
		if n == 0 {
			if _, err := s.SyncWorkspaces(ctx, it); err != nil {
				return nil, err
			}
			err = s.db.WithContext(ctx).First(&row, "integration_id = ? AND name = ?", it.ID, name).Error
		}
	}
	if err != nil {
		return nil, fmt.Errorf("no Miabi workspace named %q (use miabi_workspaces)", name)
	}
	if !row.Accessible || !row.Enabled {
		return nil, fmt.Errorf("Miabi workspace %q is not enabled for Akili", name)
	}
	return &row, nil
}

// integration picks the Miabi integration for a call: the named one, else the only one, else the
// default. "default" names the default, since that is what a model passes when it knows of no name.
func (s *Service) integration(ctx context.Context, org, name string) (*models.Integration, error) {
	var list []models.Integration
	if err := s.db.WithContext(ctx).Where("organization_id = ? AND kind = ?", org, models.KindMiabi).
		Order("created_at").Find(&list).Error; err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, errors.New("no Miabi integration is configured")
	}
	names := make([]string, len(list))
	for i := range list {
		names[i] = list[i].Name
	}
	name = strings.TrimSpace(name)
	if name != "" && !strings.EqualFold(name, "default") {
		for i := range list {
			if strings.EqualFold(list[i].Name, name) {
				return &list[i], nil
			}
		}
		return nil, fmt.Errorf("no Miabi integration named %q; available: %s (omit \"integration\" to use the default)", name, strings.Join(names, ", "))
	}
	if len(list) == 1 {
		return &list[0], nil
	}
	for i := range list {
		if list[i].Default {
			return &list[i], nil
		}
	}
	return nil, fmt.Errorf("several Miabi integrations exist (%s) and none is the default; pass \"integration\" or mark one as default", strings.Join(names, ", "))
}

// RunRemote executes an authorised Miabi tool.
func (s *Service) RunRemote(ctx context.Context, sess *models.ChatSession, tool string, input json.RawMessage) proto.RemoteResult {
	out, err := s.run(ctx, sess, tool, input)
	if err != nil {
		if out != "" {
			out += "\n"
		}
		return proto.RemoteResult{Output: out + "error: " + err.Error(), IsError: true}
	}
	return proto.RemoteResult{Output: out}
}

func (s *Service) run(ctx context.Context, sess *models.ChatSession, tool string, input json.RawMessage) (string, error) {
	var in struct {
		Workspace   string `json:"workspace"`
		App         string `json:"app"`
		Integration string `json:"integration"`
		Lines       int    `json:"lines"`
		Limit       int    `json:"limit"`
		Tag         string `json:"tag"`
		ReleaseID   int64  `json:"release_id"`
		Deployment  int64  `json:"deployment"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return "", err
	}
	it, err := s.integration(ctx, sess.OrganizationID, in.Integration)
	if err != nil {
		return "", err
	}
	base, err := s.ClientFor(it)
	if err != nil {
		return "", err
	}
	if tool == proto.ToolMiabiWorkspaces {
		return s.listWorkspaces(ctx, it)
	}
	ws, err := s.workspace(ctx, it, in.Workspace)
	if err != nil {
		return "", err
	}
	c := base.In(ws.Name)
	scope := &target{it: it, ws: ws}
	if out, handled, err := s.runWorkspaceOp(ctx, sess, scope, c, tool, input); handled {
		return out, err
	}
	if tool == proto.ToolMiabiApps {
		apps, err := c.Apps(ctx)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Miabi workspace %s: %d apps\n", ws.Name, len(apps))
		for _, a := range apps {
			fmt.Fprintf(&b, "- %s: %s, %s\n", a.Name, a.Status, imageOf(a))
		}
		return b.String(), nil
	}
	app, err := c.ResolveName(ctx, in.App)
	if err != nil {
		return "", err
	}
	if out, handled, err := s.runAppOp(ctx, sess, scope, c, app, tool, input); handled {
		return out, err
	}
	switch tool {
	case proto.ToolMiabiStatus:
		return s.status(ctx, c, app)
	case proto.ToolMiabiDeployments:
		ds, err := c.Deployments(ctx, app.ID)
		if err != nil {
			return "", err
		}
		limit := in.Limit
		if limit <= 0 || limit > 50 {
			limit = 10
		}
		var b strings.Builder
		for i, d := range ds {
			if i >= limit {
				break
			}
			fmt.Fprintf(&b, "#%d (id %d) %s  %s  trigger=%s  %s", d.Number, d.ID, d.Status, d.Image, d.Trigger, d.CreatedAt.Format(time.RFC3339))
			if d.Current {
				b.WriteString("  [current]")
			}
			if d.Error != "" {
				fmt.Fprintf(&b, "\n    error: %s", d.Error)
			}
			b.WriteString("\n")
		}
		if b.Len() == 0 {
			return "no deployments yet", nil
		}
		return b.String(), nil
	case proto.ToolMiabiReleases:
		rs, err := c.Releases(ctx, app.ID)
		if err != nil {
			return "", err
		}
		sortReleases(rs)
		var b strings.Builder
		for _, r := range rs {
			fmt.Fprintf(&b, "release %d  v%d  %s  %s", r.ID, r.Version, r.Image, r.CreatedAt.Format(time.RFC3339))
			if r.Active {
				b.WriteString("  [active]")
			}
			b.WriteString("\n")
		}
		return b.String(), nil
	case proto.ToolMiabiLogs:
		lines := in.Lines
		if lines <= 0 || lines > 1000 {
			lines = 200
		}
		logs, err := c.Logs(ctx, app.ID, lines)
		if err == nil && strings.TrimSpace(logs) == "" {
			return "no log lines", nil
		}
		return logs, err
	case proto.ToolMiabiDeployLogs:
		lines, status, err := c.DeployLogs(ctx, app.ID, in.Deployment)
		if errors.Is(err, ErrNotFound) {
			// Models often pass the "#N" number miabi_deployments shows before the id.
			if ds, lerr := c.Deployments(ctx, app.ID); lerr == nil {
				for _, d := range ds {
					if int64(d.Number) == in.Deployment {
						in.Deployment = d.ID
						lines, status, err = c.DeployLogs(ctx, app.ID, d.ID)
						break
					}
				}
			}
		}
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("deployment %d: %s\n%s", in.Deployment, status, strings.Join(lines, "\n")), nil
	case proto.ToolMiabiDeploy:
		d, err := c.Deploy(ctx, app.ID, in.Tag)
		if err != nil {
			return "", err
		}
		s.record(ctx, sess, "miabi.deploy", scope, app, map[string]any{"tag": in.Tag, "deployment_id": d.ID})
		return s.finish(ctx, c, app, d, "deploy")
	case proto.ToolMiabiRollback:
		target := in.ReleaseID
		if target == 0 {
			if target, err = previousRelease(ctx, c, app.ID); err != nil {
				var kept *keptRelease
				if errors.As(err, &kept) {
					s.record(ctx, sess, "miabi.rollback", scope, app, map[string]any{"noop": true, "failed_deployment": kept.deployment})
					st, _ := s.status(ctx, c, app)
					return kept.Error() + "\n\n" + st, nil
				}
				return "", err
			}
		}
		d, err := c.Rollback(ctx, app.ID, target)
		if err != nil {
			return "", err
		}
		s.record(ctx, sess, "miabi.rollback", scope, app, map[string]any{"release_id": target, "deployment_id": d.ID})
		return s.finish(ctx, c, app, d, fmt.Sprintf("rollback to release %d", target))
	case proto.ToolMiabiRestart:
		if err := c.Restart(ctx, app.ID); err != nil {
			return "", err
		}
		s.record(ctx, sess, "miabi.restart", scope, app, nil)
		time.Sleep(3 * time.Second)
		st, _ := s.status(ctx, c, app)
		return "restart requested\n\n" + st, nil
	}
	return "", fmt.Errorf("%s is not a Miabi tool", tool)
}

// target is where a tool call acts: an integration's workspace.
type target struct {
	it *models.Integration
	ws *models.MiabiWorkspace
}

func (s *Service) record(ctx context.Context, sess *models.ChatSession, action string, t *target, app *App, meta map[string]any) {
	if meta == nil {
		meta = map[string]any{}
	}
	meta["app"], meta["app_id"], meta["workspace"], meta["session_id"] = app.Name, app.ID, t.ws.Name, sess.ID
	s.audit.Best(ctx, audit.Entry{OrganizationID: sess.OrganizationID, ActorType: audit.ActorAgent, ActorID: sess.AgentID, Action: action,
		TargetType: "integration", TargetID: t.it.ID, Metadata: meta})
}

func (s *Service) listWorkspaces(ctx context.Context, it *models.Integration) (string, error) {
	var rows []models.MiabiWorkspace
	s.db.WithContext(ctx).Where("integration_id = ? AND enabled AND accessible", it.ID).Order("name").Find(&rows)
	if len(rows) == 0 {
		if _, err := s.SyncWorkspaces(ctx, it); err != nil {
			return "", err
		}
		s.db.WithContext(ctx).Where("integration_id = ? AND enabled AND accessible", it.ID).Order("name").Find(&rows)
	}
	if len(rows) == 0 {
		return "No Miabi workspace is enabled for Akili (an admin enables them on the integration).", nil
	}
	c, err := s.ClientFor(it)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Miabi workspaces (%s):\n", it.Name)
	for _, r := range rows {
		n := "?"
		if apps, err := c.In(r.Name).Apps(ctx); err == nil {
			n = strconv.Itoa(len(apps))
		}
		role := ""
		if r.Role != "" {
			role = "role " + r.Role + ", "
		}
		fmt.Fprintf(&b, "- %s (%s): %s%s apps\n", r.Name, r.DisplayName, role, n)
	}
	return b.String(), nil
}

// finish waits for a deploy or rollback and reports its outcome and the app's state afterwards.
func (s *Service) finish(ctx context.Context, c *Client, app *App, d *Deployment, what string) (string, error) {
	done, err := c.Wait(ctx, app.ID, d.ID, DeployTimeout)
	if err != nil {
		return fmt.Sprintf("%s started as deployment %d", what, d.ID), err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s: deployment #%d (id %d) %s — %s\n", what, done.Number, done.ID, done.Status, done.Image)
	if done.Status == "failed" {
		if done.Error != "" {
			fmt.Fprintf(&b, "error: %s\n", done.Error)
		}
		if lines, _, err := c.DeployLogs(ctx, app.ID, done.ID); err == nil {
			if len(lines) > 30 {
				lines = lines[len(lines)-30:]
			}
			b.WriteString("last deploy log lines:\n" + strings.Join(lines, "\n") + "\n")
		}
		return b.String(), fmt.Errorf("%s failed", what)
	}
	st, _ := s.status(ctx, c, app)
	b.WriteString("\n" + st)
	return b.String(), nil
}

func (s *Service) status(ctx context.Context, c *Client, app *App) (string, error) {
	st, err := c.Status(ctx, app.ID)
	if err != nil {
		return "", err
	}
	fresh, _ := c.ResolveName(ctx, app.Name)
	if fresh == nil {
		fresh = app
	}
	health := st.Health
	if health == "" {
		health = "none"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "app: %s (id %d)\nstatus: %s\nhealth: %s\nimage: %s\nrestarts: %d\nuptime: %ds\n", fresh.Name, fresh.ID, st.Status, health,
		imageOf(*fresh), st.RestartCount, st.UptimeSeconds)
	if ds, err := c.Deployments(ctx, app.ID); err == nil && len(ds) > 0 {
		d := ds[0]
		fmt.Fprintf(&b, "last deployment: #%d %s (%s) %s\n", d.Number, d.Status, d.Trigger, d.Image)
	}
	if rs, err := c.Routes(ctx, app.ID); err == nil {
		for _, r := range rs {
			for _, h := range r.Hosts {
				fmt.Fprintf(&b, "url: https://%s%s\n", h, r.Path)
			}
		}
	}
	return b.String(), nil
}

func imageOf(a App) string {
	if a.SourceType == "git" && a.Image == "" {
		return "git " + a.GitRepo + "@" + a.GitRef
	}
	if a.Tag != "" {
		return a.Image + ":" + a.Tag
	}
	return a.Image
}

func sortReleases(rs []Release) {
	sort.Slice(rs, func(i, j int) bool { return rs[i].CreatedAt.After(rs[j].CreatedAt) })
}

// keptRelease reports a rollback with nothing to undo: the newest deployment failed, so Miabi never
// replaced the release that was running before it.
type keptRelease struct {
	deployment int
	image      string
}

func (k *keptRelease) Error() string {
	return fmt.Sprintf("nothing to roll back: deployment #%d failed and never replaced %s, which is still the active release (pass release_id to go further back)", k.deployment, k.image)
}

// previousRelease picks the rollback target: the release of the last successful deployment before
// the current one (the version that was running before), else the newest release older than the
// active one. After a failed deployment it returns a *keptRelease instead: rolling back then would
// go past the version that was running before the change.
func previousRelease(ctx context.Context, c *Client, appID int64) (int64, error) {
	rs, err := c.Releases(ctx, appID)
	if err != nil {
		return 0, err
	}
	sortReleases(rs)
	active := ""
	for _, r := range rs {
		if r.Active {
			active = r.Image
		}
	}
	ds, derr := c.Deployments(ctx, appID)
	if derr == nil && len(ds) > 0 && ds[0].Status == "failed" && active != "" {
		return 0, &keptRelease{deployment: ds[0].Number, image: active}
	}
	if derr == nil && len(ds) > 1 {
		for _, d := range ds[1:] {
			if d.Status != "succeeded" || d.Image == active {
				continue
			}
			for _, r := range rs {
				if r.Image == d.Image && !r.Active {
					return r.ID, nil
				}
			}
		}
	}
	seenActive := false
	for _, r := range rs {
		if r.Active {
			seenActive = true
			continue
		}
		if seenActive {
			return r.ID, nil
		}
	}
	return 0, errors.New("there is no earlier release to roll back to")
}

// Apps lists a workspace's apps (the workspace must be enabled).
func (s *Service) Apps(ctx context.Context, it *models.Integration, workspace string) ([]App, error) {
	ws, err := s.workspace(ctx, it, workspace)
	if err != nil {
		return nil, err
	}
	c, err := s.ClientFor(it)
	if err != nil {
		return nil, err
	}
	return c.In(ws.Name).Apps(ctx)
}

// App finds one app of an enabled workspace by name.
func (s *Service) App(ctx context.Context, it *models.Integration, workspace, name string) (*App, error) {
	ws, err := s.workspace(ctx, it, workspace)
	if err != nil {
		return nil, err
	}
	c, err := s.ClientFor(it)
	if err != nil {
		return nil, err
	}
	return c.In(ws.Name).ResolveName(ctx, name)
}
