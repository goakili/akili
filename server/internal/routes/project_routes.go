// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package routes

import (
	"net/http"

	"github.com/goakili/akili/server/internal/dto"
	"github.com/goakili/akili/server/internal/handlers"
	"github.com/goakili/akili/server/internal/models"
	"github.com/jkaninda/okapi"
)

func (r *Router) projectRoutes() []okapi.RouteDefinition {
	g := r.group("Projects", "Repositories agents work on, forge integrations, and coding tasks.")
	hooks := r.v1.Group("/webhooks").WithTagInfo(okapi.GroupTag{Name: "Webhooks", Description: "Signed forge events."})
	return []okapi.RouteDefinition{
		{
			Method:      http.MethodGet,
			Path:        "/integrations",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.ListIntegrations,
			Summary:     "List forge integrations",
			Response:    &dto.Response[[]handlers.IntegrationView]{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/integrations",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     okapi.H(r.h.CreateIntegration),
			Summary:     "Add a forge integration (Gitea or GitHub)",
			Request:     &handlers.IntegrationRequest{},
		},
		{
			Method:      http.MethodPut,
			Path:        "/integrations/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     okapi.H(r.h.UpdateIntegration),
			Summary:     "Update an integration",
			Request:     &handlers.IntegrationRequest{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/integrations/{id}/default",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.SetDefaultIntegration,
			Summary:     "Make a Miabi integration the default for tool calls",
			Response:    &dto.Response[handlers.IntegrationView]{},
		},
		{
			Method:      http.MethodDelete,
			Path:        "/integrations/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.DeleteIntegration,
			Summary:     "Delete an integration",
		},
		{
			Method:      http.MethodPost,
			Path:        "/integrations/{id}/test",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.TestIntegration,
			Summary:     "Check an integration's credentials",
			Response:    &dto.Response[handlers.TestResult]{},
		},
		{
			Method:      http.MethodGet,
			Path:        "/integrations/{id}/miabi-workspaces",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.ListMiabiWorkspaces,
			Summary:     "Miabi workspaces the key can reach",
			Response:    &dto.Response[[]models.MiabiWorkspace]{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/integrations/{id}/miabi-workspaces/sync",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.SyncMiabiWorkspaces,
			Summary:     "Re-read the workspaces from Miabi",
			Response:    &dto.Response[[]models.MiabiWorkspace]{},
		},
		{
			Method:      http.MethodPut,
			Path:        "/integrations/{id}/miabi-workspaces/{ws}",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     okapi.H(r.h.UpdateMiabiWorkspace),
			Summary:     "Enable or disable a Miabi workspace for agents and events",
			Request:     &handlers.MiabiWorkspaceRequest{},
			Response:    &dto.Response[models.MiabiWorkspace]{},
		},
		{
			Method:      http.MethodGet,
			Path:        "/projects",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.ListProjects,
			Summary:     "List projects",
			Response:    &dto.Response[[]models.Project]{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/projects",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     okapi.H(r.h.CreateProject),
			Summary:     "Connect or create a repository (optionally scaffold it from a template)",
			Request:     &handlers.ProjectRequest{},
			Response:    &dto.Response[handlers.ProjectCreated]{},
		},
		{
			Method:      http.MethodGet,
			Path:        "/projects/resolve",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.ResolveProject,
			Summary:     "Find the project for a git remote URL (?remote=)",
			Response:    &dto.Response[models.Project]{},
		},
		{
			Method:      http.MethodGet,
			Path:        "/projects/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.GetProject,
			Summary:     "Get a project",
			Response:    &dto.Response[models.Project]{},
		},
		{
			Method:      http.MethodPut,
			Path:        "/projects/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     okapi.H(r.h.UpdateProject),
			Summary:     "Update a project",
			Request:     &handlers.ProjectRequest{},
		},
		{
			Method:      http.MethodDelete,
			Path:        "/projects/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.DeleteProject,
			Summary:     "Delete a project (the repository is kept)",
		},
		{
			Method:      http.MethodPost,
			Path:        "/projects/{id}/maintenance",
			Group:       g,
			Middlewares: r.guard(models.RoleOperator),
			Handler:     okapi.H(r.h.AddMaintenance),
			Summary:     "Schedule a maintenance preset",
			Request:     &handlers.PresetRequest{},
		},
		{
			Method:      http.MethodGet,
			Path:        "/project-templates",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.ListProjectTemplates,
			Summary:     "Project templates and maintenance presets",
			Response:    &dto.Response[handlers.ProjectTemplates]{},
		},
		{
			Method:      http.MethodGet,
			Path:        "/tasks/{id}/diff",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.TaskDiff,
			Summary:     "Pull request diff of a coding task",
		},
		{
			Method:  http.MethodPost,
			Path:    "/forge/{id}",
			Group:   hooks,
			Handler: r.h.ForgeWebhook,
			Summary: "Forge webhook (HMAC-signed issue events)",
		},
	}
}
