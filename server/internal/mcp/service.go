// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/audit"
	"github.com/goakili/akili/server/internal/crypto"
	"github.com/goakili/akili/server/internal/models"
	"github.com/jkaninda/logger"
	"gorm.io/gorm"
)

// MaxOutput bounds what one MCP call returns to the model.
const MaxOutput = 64 << 10

// CallTimeout bounds one tool call.
var CallTimeout = 5 * time.Minute

// NameRE is an MCP server's name: it becomes part of tool names.
var NameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// Service manages MCP servers and runs their tools.
type Service struct {
	db       *gorm.DB
	box      *crypto.Box
	audit    *audit.Logger
	commands []string // stdio commands an admin may configure (AKILI_MCP_COMMANDS)
	binDir   string   // searched before PATH (AKILI_MCP_BIN_DIR)
	workDir  string

	mu      sync.Mutex
	clients map[string]*cached
}

type cached struct {
	key string // server id + updated_at: an edit starts a fresh connection
	c   *Client
}

// New returns the service. commands is the allowlist of executables for stdio servers, looked up
// in binDir (if set) before PATH.
func New(db *gorm.DB, box *crypto.Box, a *audit.Logger, commands []string, binDir string) *Service {
	dir := filepath.Join(os.TempDir(), "akili-mcp")
	_ = os.MkdirAll(dir, 0o700)
	return &Service{db: db, box: box, audit: a, commands: commands, binDir: binDir, workDir: dir, clients: map[string]*cached{}}
}

// ErrCommandNotAllowed means a stdio command is not in AKILI_MCP_COMMANDS.
var ErrCommandNotAllowed = errors.New("command not allowed")

// CheckCommand verifies a stdio command against the allowlist (by base name).
func (s *Service) CheckCommand(command string) error {
	if !slices.Contains(s.commands, filepath.Base(command)) || strings.ContainsAny(command, " ;|&$`") {
		return fmt.Errorf("%w: %q is not in AKILI_MCP_COMMANDS (%s)", ErrCommandNotAllowed, command, strings.Join(s.commands, ", "))
	}
	return nil
}

// resolveCommand finds a stdio command in binDir, then PATH. A binDir binary the server could
// rewrite is refused: it runs with integration keys, so a compromised server could swap it.
func (s *Service) resolveCommand(command string) (string, error) {
	if s.binDir != "" && !strings.ContainsRune(command, filepath.Separator) {
		p := filepath.Join(s.binDir, command)
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			if writable(s.binDir) || writable(p) {
				return "", fmt.Errorf("%s is writable by the control plane; mount AKILI_MCP_BIN_DIR read-only", p)
			}
			return p, nil
		}
	}
	path, err := exec.LookPath(command)
	if err != nil {
		return "", fmt.Errorf("%q is not installed on the control plane: add it to the image, or to AKILI_MCP_BIN_DIR", command)
	}
	return path, nil
}

func (s *Service) childPath() string {
	if s.binDir == "" {
		return os.Getenv("PATH")
	}
	return s.binDir + string(os.PathListSeparator) + os.Getenv("PATH")
}

// Env decrypts a server's environment (stdio) or headers (http).
func (s *Service) Env(srv *models.MCPServer) (map[string]string, error) {
	out := map[string]string{}
	if srv.EnvEnc == "" {
		return out, nil
	}
	raw, err := s.box.Decrypt(srv.EnvEnc)
	if err != nil {
		return nil, err
	}
	return out, json.Unmarshal([]byte(raw), &out)
}

// SealEnv encrypts an environment for storage and returns it with its sorted keys.
func (s *Service) SealEnv(env map[string]string) (string, []string, error) {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if len(env) == 0 {
		return "", keys, nil
	}
	b, _ := json.Marshal(env)
	enc, err := s.box.Encrypt(string(b))
	return enc, keys, err
}

// connect returns a live client for a server, starting it if needed.
func (s *Service) connect(ctx context.Context, srv *models.MCPServer) (*Client, error) {
	key := srv.ID + srv.UpdatedAt.String()
	if srv.IntegrationID != nil {
		// A changed key or CA on the integration must restart the preset's process too.
		var it models.Integration
		if s.db.WithContext(ctx).Select("updated_at").First(&it, "id = ?", *srv.IntegrationID).Error == nil {
			key += it.UpdatedAt.String()
		}
	}
	s.mu.Lock()
	if c, ok := s.clients[srv.ID]; ok {
		if c.key == key {
			s.mu.Unlock()
			return c.c, nil
		}
		_ = c.c.Close()
		delete(s.clients, srv.ID)
	}
	s.mu.Unlock()
	env, err := s.Env(srv)
	if err != nil {
		return nil, err
	}
	var c *Client
	switch srv.Transport {
	case "stdio":
		if err := s.CheckCommand(srv.Command); err != nil {
			return nil, err
		}
		path, err := s.resolveCommand(srv.Command)
		if err != nil {
			return nil, err
		}
		home := filepath.Join(s.workDir, srv.ID)
		if err := os.MkdirAll(home, 0o700); err != nil {
			return nil, err
		}
		args := append([]string(nil), srv.Args...)
		if srv.IntegrationID != nil {
			if err := s.miabiEnv(ctx, srv, env, home); err != nil {
				return nil, err
			}
			args = []string{"mcp"}
			if srv.AllowWrite {
				args = append(args, "--allow-write")
			}
		}
		// A minimal environment: PATH to find helpers, a private HOME, and the configured variables.
		vars := []string{"PATH=" + s.childPath(), "HOME=" + home}
		for k, v := range env {
			vars = append(vars, k+"="+v)
		}
		if c, err = StartStdio(ctx, path, args, vars, home); err != nil {
			return nil, err
		}
	case "http":
		if c, err = DialHTTP(ctx, srv.URL, env); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown transport %q", srv.Transport)
	}
	s.mu.Lock()
	s.clients[srv.ID] = &cached{key: key, c: c}
	s.mu.Unlock()
	return c, nil
}

// miabiEnv points `miabi mcp` at a Miabi integration: its URL and key, and a private config file so
// the CLI never reads someone's ~/.miabi.
func (s *Service) miabiEnv(ctx context.Context, srv *models.MCPServer, env map[string]string, home string) error {
	var it models.Integration
	if err := s.db.WithContext(ctx).First(&it, "id = ? AND organization_id = ? AND kind = ?", *srv.IntegrationID, srv.OrganizationID, models.KindMiabi).Error; err != nil {
		return errors.New("the Miabi integration of this MCP server no longer exists")
	}
	key, err := s.box.Decrypt(it.TokenEnc)
	if err != nil {
		return err
	}
	env["MIABI_SERVER"], env["MIABI_TOKEN"], env["MIABI_CONFIG"] = it.BaseURL, key, filepath.Join(home, "miabi.yaml")
	if it.CACert != "" {
		ca := filepath.Join(home, "miabi-ca.pem")
		if err := os.WriteFile(ca, []byte(it.CACert), 0o600); err != nil {
			return err
		}
		env["MIABI_CA"] = ca
	}
	return nil
}

// drop closes a server's connection (after an error, so the next call reconnects).
func (s *Service) drop(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.clients[id]; ok {
		_ = c.c.Close()
		delete(s.clients, id)
	}
}

// Sync lists a server's tools and records them. New read-only tools start enabled at low risk; the
// rest stay disabled until an admin enables them and sets a risk.
func (s *Service) Sync(ctx context.Context, srv *models.MCPServer) ([]models.MCPTool, error) {
	c, err := s.connect(ctx, srv)
	if err == nil {
		var tools []Tool
		if tools, err = c.ListTools(ctx); err == nil {
			err = s.store(ctx, srv, tools)
		}
	}
	now := time.Now().UTC()
	if err != nil {
		s.drop(srv.ID)
		s.db.WithContext(ctx).Model(&models.MCPServer{}).Where("id = ?", srv.ID).UpdateColumn("last_error", truncate(err.Error(), 480))
		return nil, err
	}
	s.db.WithContext(ctx).Model(&models.MCPServer{}).Where("id = ?", srv.ID).UpdateColumns(map[string]any{"last_error": "", "synced_at": now})
	s.Refresh(ctx)
	var out []models.MCPTool
	return out, s.db.WithContext(ctx).Where("server_id = ?", srv.ID).Order("name").Find(&out).Error
}

var toolNameRE = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

func (s *Service) store(ctx context.Context, srv *models.MCPServer, tools []Tool) error {
	seen := map[string]bool{}
	for _, t := range tools {
		if !toolNameRE.MatchString(t.Name) {
			logger.Warn("mcp tool with an unusable name skipped", "server", srv.Name, "tool", t.Name)
			continue
		}
		seen[t.Name] = true
		ro := t.Annotations != nil && t.Annotations.ReadOnlyHint
		destructive := t.Annotations != nil && t.Annotations.DestructiveHint
		schema := t.InputSchema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object"}`)
		}
		var row models.MCPTool
		err := s.db.WithContext(ctx).First(&row, "server_id = ? AND name = ?", srv.ID, t.Name).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			row = models.MCPTool{Base: models.Base{ID: models.NewID("mct"), OrganizationID: srv.OrganizationID}, ServerID: srv.ID, Name: t.Name,
				Enabled: ro && !destructive}
			if row.Enabled {
				row.Risk = "low"
			}
		} else if err != nil {
			return err
		}
		// A tool that stops being read-only loses its automatic low risk.
		if row.ReadOnly && !ro && row.Risk == "low" {
			row.Enabled, row.Risk = false, ""
		}
		row.Description, row.InputSchema, row.ReadOnly, row.Destructive = t.Description, schema, ro, destructive
		if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
			return err
		}
	}
	var existing []models.MCPTool
	s.db.WithContext(ctx).Where("server_id = ?", srv.ID).Find(&existing)
	for _, e := range existing {
		if !seen[e.Name] {
			s.db.WithContext(ctx).Delete(&e)
		}
	}
	return nil
}

// Refresh publishes the enabled tools of every enabled server to the tool registry, so policy
// checks and sessions see them. Every replica runs it.
func (s *Service) Refresh(ctx context.Context) {
	var servers []models.MCPServer
	s.db.WithContext(ctx).Find(&servers)
	for _, srv := range servers {
		prefix := proto.MCPToolName(srv.Name, "")
		if !srv.Enabled {
			proto.SetDynamicTools(prefix, nil)
			continue
		}
		proto.SetDynamicTools(prefix, s.toolsOf(ctx, &srv))
	}
}

func (s *Service) toolsOf(ctx context.Context, srv *models.MCPServer) []proto.DynamicTool {
	var rows []models.MCPTool
	s.db.WithContext(ctx).Where("server_id = ? AND enabled AND risk <> ''", srv.ID).Order("name").Find(&rows)
	out := make([]proto.DynamicTool, 0, len(rows))
	for _, r := range rows {
		risk, err := proto.ParseRisk(r.Risk)
		if err != nil {
			continue
		}
		out = append(out, proto.DynamicTool{Name: proto.MCPToolName(srv.Name, r.Name), Description: fmt.Sprintf("[%s via MCP] %s", srv.Name, r.Description),
			InputSchema: r.InputSchema, Risk: risk})
	}
	return out
}

// Tools returns an organization's usable MCP tools, for offering to sessions.
func (s *Service) Tools(ctx context.Context, org string) []proto.DynamicTool {
	var servers []models.MCPServer
	s.db.WithContext(ctx).Where("organization_id = ? AND enabled", org).Find(&servers)
	var out []proto.DynamicTool
	for i := range servers {
		out = append(out, s.toolsOf(ctx, &servers[i])...)
	}
	return out
}

// RunRefresh keeps the registry in step with edits made on other replicas.
func (s *Service) RunRefresh(ctx context.Context) {
	s.Refresh(ctx)
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			s.mu.Lock()
			for id, c := range s.clients {
				_ = c.c.Close()
				delete(s.clients, id)
			}
			s.mu.Unlock()
			return
		case <-t.C:
			s.Refresh(ctx)
		}
	}
}

// RunRemote executes an MCP tool for a session (a remote tool runner).
func (s *Service) RunRemote(ctx context.Context, sess *models.ChatSession, tool string, input json.RawMessage) proto.RemoteResult {
	rest, ok := strings.CutPrefix(tool, "mcp__")
	server, name, ok2 := strings.Cut(rest, "__")
	if !ok || !ok2 {
		return proto.RemoteResult{Output: "error: not an MCP tool", IsError: true}
	}
	var srv models.MCPServer
	if err := s.db.WithContext(ctx).First(&srv, "name = ? AND organization_id = ? AND enabled", server, sess.OrganizationID).Error; err != nil {
		return proto.RemoteResult{Output: "error: MCP server " + server + " is not available", IsError: true}
	}
	var row models.MCPTool
	if err := s.db.WithContext(ctx).First(&row, "server_id = ? AND name = ? AND enabled AND risk <> ''", srv.ID, name).Error; err != nil {
		return proto.RemoteResult{Output: "error: MCP tool " + name + " is not enabled", IsError: true}
	}
	s.audit.Best(ctx, audit.Entry{OrganizationID: sess.OrganizationID, ActorType: audit.ActorAgent, ActorID: sess.AgentID, Action: "mcp.call",
		TargetType: "mcp_server", TargetID: srv.ID, Metadata: map[string]any{"server": srv.Name, "tool": name, "risk": row.Risk, "session_id": sess.ID}})
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	c, err := s.connect(cctx, &srv)
	if err != nil {
		return proto.RemoteResult{Output: "error: " + err.Error(), IsError: true}
	}
	out, isErr, err := c.CallTool(cctx, name, input)
	if err != nil {
		s.drop(srv.ID) // the process may be wedged or gone: reconnect next time
		return proto.RemoteResult{Output: "error: " + err.Error(), IsError: true}
	}
	if len(out) > MaxOutput {
		out = out[:MaxOutput] + "\n… [truncated]"
	}
	return proto.RemoteResult{Output: out, IsError: isErr}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
