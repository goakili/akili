// Copyright (c) 2026 Jonas Kaninda.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package routes

import (
	"net/http"

	"github.com/goakili/akili/server/internal/dto"
	"github.com/goakili/akili/server/internal/handlers"
	"github.com/goakili/akili/server/internal/models"
	"github.com/jkaninda/okapi"
)

func (r *Router) mcpRoutes() []okapi.RouteDefinition {
	g := r.group("MCP", "MCP servers whose tools agents can call through the control plane.")
	return []okapi.RouteDefinition{
		{
			Method:      http.MethodGet,
			Path:        "/mcp-servers",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.ListMCPServers,
			Summary:     "List MCP servers",
			Response:    &dto.Response[[]models.MCPServer]{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/mcp-servers",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     okapi.H(r.h.CreateMCPServer),
			Summary:     "Add an MCP server (or the Miabi preset) and list its tools",
			Request:     &handlers.MCPServerRequest{},
			Response:    &dto.Response[handlers.MCPServerView]{},
		},
		{
			Method:      http.MethodPut,
			Path:        "/mcp-servers/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     okapi.H(r.h.UpdateMCPServer),
			Summary:     "Update an MCP server",
			Request:     &handlers.MCPServerRequest{},
		},
		{
			Method:      http.MethodDelete,
			Path:        "/mcp-servers/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.DeleteMCPServer,
			Summary:     "Delete an MCP server",
		},
		{
			Method:      http.MethodPost,
			Path:        "/mcp-servers/{id}/sync",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.SyncMCPServer,
			Summary:     "Re-read a server's tools",
			Response:    &dto.Response[[]models.MCPTool]{},
		},
		{
			Method:      http.MethodGet,
			Path:        "/mcp-servers/{id}/tools",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.ListMCPTools,
			Summary:     "A server's tools",
			Response:    &dto.Response[[]models.MCPTool]{},
		},
		{
			Method:      http.MethodPut,
			Path:        "/mcp-servers/{id}/tools/{tool}",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     okapi.H(r.h.UpdateMCPTool),
			Summary:     "Enable a tool for agents and set its risk",
			Request:     &handlers.MCPToolRequest{},
			Response:    &dto.Response[models.MCPTool]{},
		},
	}
}
