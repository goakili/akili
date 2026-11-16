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

	"github.com/goakili/akili/server/internal/autonomy"
	"github.com/goakili/akili/server/internal/dto"
	"github.com/goakili/akili/server/internal/handlers"
	"github.com/goakili/akili/server/internal/models"
	"github.com/jkaninda/okapi"
)

func (r *Router) autonomyRoutes() []okapi.RouteDefinition {
	g := r.group("Autonomy", "Grants, change windows, runbooks and suggestions: what agents may do without asking.")
	return []okapi.RouteDefinition{
		{
			Method:      http.MethodGet,
			Path:        "/grants",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.ListGrants,
			Summary:     "List grants (?agent_id=&active=true)",
			Response:    &dto.Response[[]models.ApprovalGrant]{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/grants",
			Group:       g,
			Middlewares: r.guard(models.RoleOperator),
			Handler:     okapi.H(r.h.CreateGrant),
			Summary:     "Grant calls matching these bounds to run without approval, for a limited time (signed-in people only)",
			Request:     &handlers.GrantRequest{},
			Response:    &dto.Response[models.ApprovalGrant]{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/grants/{id}/revoke",
			Group:       g,
			Middlewares: r.guard(models.RoleOperator),
			Handler:     r.h.RevokeGrant,
			Summary:     "Revoke a grant",
			Response:    &dto.Response[models.ApprovalGrant]{},
		},
		{
			Method:      http.MethodGet,
			Path:        "/change-windows",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.ListWindows,
			Summary:     "List change windows and freezes",
			Response:    &dto.Response[[]models.ChangeWindow]{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/change-windows",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     okapi.H(r.h.CreateWindow),
			Summary:     "Add a change window or freeze",
			Request:     &handlers.WindowRequest{},
			Response:    &dto.Response[models.ChangeWindow]{},
		},
		{
			Method:      http.MethodPut,
			Path:        "/change-windows/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     okapi.H(r.h.UpdateWindow),
			Summary:     "Update a change window or freeze",
			Request:     &handlers.WindowRequest{},
			Response:    &dto.Response[models.ChangeWindow]{},
		},
		{
			Method:      http.MethodDelete,
			Path:        "/change-windows/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.DeleteWindow,
			Summary:     "Delete a change window or freeze",
		},
		{
			Method:      http.MethodGet,
			Path:        "/runbooks",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.ListRunbooks,
			Summary:     "List runbooks",
			Response:    &dto.Response[[]models.Runbook]{},
		},
		{
			Method:      http.MethodGet,
			Path:        "/runbooks/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.GetRunbook,
			Summary:     "Get a runbook",
			Response:    &dto.Response[models.Runbook]{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/runbooks",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     okapi.H(r.h.CreateRunbook),
			Summary:     "Add a runbook (a change plan template with bounded {{parameters}})",
			Request:     &handlers.RunbookRequest{},
			Response:    &dto.Response[models.Runbook]{},
		},
		{
			Method:      http.MethodPut,
			Path:        "/runbooks/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     okapi.H(r.h.UpdateRunbook),
			Summary:     "Update a runbook; changing its calls or bounds invalidates its grants",
			Request:     &handlers.RunbookRequest{},
			Response:    &dto.Response[models.Runbook]{},
		},
		{
			Method:      http.MethodDelete,
			Path:        "/runbooks/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.DeleteRunbook,
			Summary:     "Delete a runbook and revoke its grants",
		},
		{
			Method:      http.MethodGet,
			Path:        "/changes/{id}/runbook-draft",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.RunbookDraft,
			Summary:     "A runbook draft from a recorded change, to generalise with {{parameters}}",
			Response:    &dto.Response[autonomy.RunbookInput]{},
		},
		{
			Method:      http.MethodGet,
			Path:        "/agents/{id}/autonomy",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.GetAgentAutonomy,
			Summary:     "The agent's active grants and suggestions from its approval history",
			Response:    &dto.Response[handlers.AgentAutonomy]{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/agents/{id}/autonomy/suggestions/{key}/accept",
			Group:       g,
			Middlewares: r.guard(models.RoleOperator),
			Handler:     okapi.H(r.h.AcceptSuggestion),
			Summary:     "Turn a suggestion into a grant (signed-in people only)",
			Request:     &handlers.AcceptSuggestionRequest{},
			Response:    &dto.Response[models.ApprovalGrant]{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/agents/{id}/autonomy/suggestions/{key}/dismiss",
			Group:       g,
			Middlewares: r.guard(models.RoleOperator),
			Handler:     r.h.DismissSuggestion,
			Summary:     "Hide a suggestion until there is newer evidence",
		},
		{
			Method:      http.MethodGet,
			Path:        "/system/autonomy",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.GetAutonomySettings,
			Summary:     "Grant lifetime, stop conditions and suggestion thresholds",
			Response:    &dto.Response[models.AutonomySettings]{},
		},
		{
			Method:      http.MethodPut,
			Path:        "/system/autonomy",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     okapi.H(r.h.SetAutonomySettings),
			Summary:     "Change grant lifetime, stop conditions and suggestion thresholds (0 = default)",
			Request:     &handlers.AutonomySettingsRequest{},
			Response:    &dto.Response[models.AutonomySettings]{},
		},
	}
}
