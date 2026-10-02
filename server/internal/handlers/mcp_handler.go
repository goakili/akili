// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package handlers

import (
	"context"
	"strings"
	"time"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/mcp"
	"github.com/goakili/akili/server/internal/middlewares"
	"github.com/goakili/akili/server/internal/models"
	"github.com/jkaninda/okapi"
)

// MCPServerRequest creates or updates an MCP server. Env values are write-only: null keeps them.
type MCPServerRequest struct {
	Body struct {
		Name          string            `json:"name" required:"true" description:"lowercase name; tools become mcp__<name>__<tool>"`
		Transport     string            `json:"transport" description:"stdio or http (default stdio)"`
		Command       string            `json:"command" description:"stdio: an executable allowed by AKILI_MCP_COMMANDS"`
		Args          []string          `json:"args"`
		URL           string            `json:"url" description:"http: the Streamable HTTP endpoint"`
		Env           map[string]string `json:"env" description:"stdio: environment variables; http: request headers (e.g. Authorization)"`
		IntegrationID *string           `json:"integration_id" description:"Miabi preset: run miabi mcp with this Miabi integration's URL and key"`
		AllowWrite    bool              `json:"allow_write" description:"Miabi preset: also expose mutating tools (they stay disabled until given a risk)"`
		Enabled       *bool             `json:"enabled"`
	} `json:"body"`
}

// MCPToolRequest enables a tool for agents and sets its risk.
type MCPToolRequest struct {
	Body struct {
		Enabled bool   `json:"enabled"`
		Risk    string `json:"risk" description:"low, medium, high or critical; required to enable"`
	} `json:"body"`
}

// ListMCPServers lists MCP servers.
func (h *Handlers) ListMCPServers(c *okapi.Context) error {
	var out []models.MCPServer
	h.DB.Where("organization_id = ?", middlewares.OrgID(c)).Order("name").Find(&out)
	return ok(c, out)
}

func (h *Handlers) saveMCPServer(c *okapi.Context, srv *models.MCPServer, req *MCPServerRequest, isNew bool) error {
	b := req.Body
	srv.Name = strings.TrimSpace(b.Name)
	if !mcp.NameRE.MatchString(srv.Name) {
		return c.AbortBadRequest("name must be lowercase letters, digits and dashes (max 32)")
	}
	var clash int64
	h.DB.Model(&models.MCPServer{}).Where("name = ? AND id <> ?", srv.Name, srv.ID).Count(&clash)
	if clash > 0 {
		return c.AbortConflict("an MCP server with this name exists")
	}
	srv.Transport = strings.TrimSpace(b.Transport)
	if srv.Transport == "" {
		srv.Transport = "stdio"
	}
	srv.IntegrationID, srv.AllowWrite = emptyNil(b.IntegrationID), b.AllowWrite
	switch {
	case srv.IntegrationID != nil:
		var it models.Integration
		if err := h.DB.First(&it, "id = ? AND organization_id = ? AND kind = ?", *srv.IntegrationID, srv.OrganizationID, models.KindMiabi).Error; err != nil {
			return c.AbortBadRequest("unknown Miabi integration")
		}
		srv.Transport, srv.Command, srv.Args, srv.URL = "stdio", "miabi", []string{"mcp"}, ""
	case srv.Transport == "stdio":
		srv.Command, srv.Args, srv.URL = strings.TrimSpace(b.Command), nonNilList(b.Args), ""
	case srv.Transport == "http":
		srv.URL, srv.Command, srv.Args = strings.TrimSpace(b.URL), "", []string{}
		if !strings.HasPrefix(srv.URL, "https://") && !(h.Cfg.IsDev() && strings.HasPrefix(srv.URL, "http://")) {
			return c.AbortBadRequest("url must be https")
		}
	default:
		return c.AbortBadRequest("transport must be stdio or http")
	}
	if srv.Transport == "stdio" {
		if err := h.MCP.CheckCommand(srv.Command); err != nil {
			return c.AbortBadRequest(err.Error())
		}
	}
	if b.Env != nil {
		for k := range b.Env {
			if k == "" || strings.ContainsAny(k, "=\x00") {
				return c.AbortBadRequest("invalid variable name")
			}
		}
		enc, keys, err := h.MCP.SealEnv(b.Env)
		if err != nil {
			return c.AbortInternalServerError("encryption failed", err)
		}
		srv.EnvEnc, srv.EnvKeys = enc, keys
	}
	if srv.EnvKeys == nil {
		srv.EnvKeys = []string{}
	}
	if b.Enabled != nil {
		srv.Enabled = *b.Enabled
	}
	return h.DB.Save(srv).Error
}

func (h *Handlers) syncMCP(ctx context.Context, srv *models.MCPServer) ([]models.MCPTool, error) {
	sctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return h.MCP.Sync(sctx, srv)
}

// MCPServerView is a server with its tools.
type MCPServerView struct {
	Server models.MCPServer `json:"server"`
	Tools  []models.MCPTool `json:"tools"`
	Error  string           `json:"error,omitempty"`
}

// CreateMCPServer adds an MCP server and lists its tools.
func (h *Handlers) CreateMCPServer(c *okapi.Context, req *MCPServerRequest) error {
	srv := &models.MCPServer{Base: models.Base{ID: models.NewID("mcs"), OrganizationID: middlewares.OrgID(c)}, Enabled: true, CreatedBy: middlewares.UserID(c)}
	if err := h.saveMCPServer(c, srv, req, true); err != nil {
		return err
	}
	h.record(c, "mcp_server.create", "mcp_server", srv.ID, map[string]any{"name": srv.Name, "transport": srv.Transport, "command": srv.Command, "url": srv.URL})
	out := MCPServerView{Server: *srv}
	tools, err := h.syncMCP(c.Request().Context(), srv)
	if err != nil {
		out.Error = err.Error()
	}
	out.Tools = tools
	return created(c, out)
}

// UpdateMCPServer edits an MCP server.
func (h *Handlers) UpdateMCPServer(c *okapi.Context, req *MCPServerRequest) error {
	var srv models.MCPServer
	if err := h.DB.First(&srv, "id = ? AND organization_id = ?", c.Param("id"), middlewares.OrgID(c)).Error; err != nil {
		return c.AbortNotFound("MCP server not found")
	}
	old := srv.Name
	if err := h.saveMCPServer(c, &srv, req, false); err != nil {
		return err
	}
	if old != srv.Name {
		proto.SetDynamicTools(proto.MCPToolName(old, ""), nil)
	}
	h.MCP.Refresh(c.Request().Context())
	h.record(c, "mcp_server.update", "mcp_server", srv.ID, map[string]any{"name": srv.Name, "enabled": srv.Enabled})
	return ok(c, srv)
}

// DeleteMCPServer removes a server and its tools.
func (h *Handlers) DeleteMCPServer(c *okapi.Context) error {
	var srv models.MCPServer
	if err := h.DB.First(&srv, "id = ? AND organization_id = ?", c.Param("id"), middlewares.OrgID(c)).Error; err != nil {
		return c.AbortNotFound("MCP server not found")
	}
	h.DB.Where("server_id = ?", srv.ID).Delete(&models.MCPTool{})
	h.DB.Delete(&srv)
	proto.SetDynamicTools(proto.MCPToolName(srv.Name, ""), nil)
	h.record(c, "mcp_server.delete", "mcp_server", srv.ID, map[string]any{"name": srv.Name})
	return message(c, "deleted")
}

// SyncMCPServer re-reads a server's tools.
func (h *Handlers) SyncMCPServer(c *okapi.Context) error {
	var srv models.MCPServer
	if err := h.DB.First(&srv, "id = ? AND organization_id = ?", c.Param("id"), middlewares.OrgID(c)).Error; err != nil {
		return c.AbortNotFound("MCP server not found")
	}
	tools, err := h.syncMCP(c.Request().Context(), &srv)
	if err != nil {
		return c.AbortBadRequest("MCP: " + err.Error())
	}
	h.record(c, "mcp_server.sync", "mcp_server", srv.ID, map[string]any{"tools": len(tools)})
	return ok(c, tools)
}

// ListMCPTools lists a server's tools.
func (h *Handlers) ListMCPTools(c *okapi.Context) error {
	var out []models.MCPTool
	h.DB.Where("server_id = ? AND organization_id = ?", c.Param("id"), middlewares.OrgID(c)).Order("name").Find(&out)
	return ok(c, out)
}

// UpdateMCPTool enables or disables a tool and sets its risk.
func (h *Handlers) UpdateMCPTool(c *okapi.Context, req *MCPToolRequest) error {
	var t models.MCPTool
	if err := h.DB.First(&t, "id = ? AND server_id = ? AND organization_id = ?", c.Param("tool"), c.Param("id"), middlewares.OrgID(c)).Error; err != nil {
		return c.AbortNotFound("tool not found")
	}
	risk := strings.TrimSpace(req.Body.Risk)
	if risk != "" {
		if _, err := proto.ParseRisk(risk); err != nil {
			return c.AbortBadRequest("risk must be low, medium, high or critical")
		}
	}
	if req.Body.Enabled && risk == "" {
		return c.AbortBadRequest("set a risk before enabling a tool")
	}
	if risk == "low" && !t.ReadOnly {
		// Low risk runs without approval from L1: only for tools the server says change nothing.
		return c.AbortBadRequest("only read-only tools can be low risk")
	}
	t.Enabled, t.Risk = req.Body.Enabled, risk
	if err := h.DB.Model(&t).Select("enabled", "risk").Updates(&t).Error; err != nil {
		return c.AbortInternalServerError("update failed", err)
	}
	h.MCP.Refresh(c.Request().Context())
	h.record(c, "mcp_tool.update", "mcp_tool", t.ID, map[string]any{"tool": t.Name, "enabled": t.Enabled, "risk": t.Risk})
	return ok(c, t)
}
