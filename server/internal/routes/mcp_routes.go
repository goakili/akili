// Copyright (c) 2026 Jonas Kaninda.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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
