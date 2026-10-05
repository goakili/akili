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

func (r *Router) opsRoutes() []okapi.RouteDefinition {
	g := r.group("Operations", "Change plans, alert routes and recorded terminals.")
	hooks := r.v1.Group("/webhooks").WithTagInfo(okapi.GroupTag{Name: "Webhooks", Description: "Signed forge events and alert routes."})
	return []okapi.RouteDefinition{
		{
			Method:      http.MethodGet,
			Path:        "/changes",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.ListChanges,
			Summary:     "List change plans",
			Response:    &dto.PageResponse[models.Change]{},
			Options:     pageDocs(),
		},
		{
			Method:      http.MethodGet,
			Path:        "/changes/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.GetChange,
			Summary:     "A change plan with its step outcomes",
			Response:    &dto.Response[models.Change]{},
		},
		{
			Method:      http.MethodGet,
			Path:        "/alert-routes",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.ListAlertRoutes,
			Summary:     "List alert routes",
			Response:    &dto.Response[[]models.AlertRoute]{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/alert-routes",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     okapi.H(r.h.CreateAlertRoute),
			Summary:     "Create an alert route (returns its secret webhook URL once)",
			Request:     &handlers.AlertRouteRequest{},
			Response:    &dto.Response[handlers.AlertRouteCreated]{},
		},
		{
			Method:      http.MethodPut,
			Path:        "/alert-routes/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     okapi.H(r.h.UpdateAlertRoute),
			Summary:     "Update an alert route",
			Request:     &handlers.AlertRouteRequest{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/alert-routes/{id}/rotate",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.RotateAlertRoute,
			Summary:     "Issue a new webhook URL (the old one stops working)",
		},
		{
			Method:      http.MethodDelete,
			Path:        "/alert-routes/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.DeleteAlertRoute,
			Summary:     "Delete an alert route",
		},
		{
			Method:      http.MethodGet,
			Path:        "/terminals",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.ListTerminals,
			Summary:     "Recorded terminal sessions",
			Response:    &dto.PageResponse[models.TerminalSession]{},
			Options:     pageDocs(),
		},
		{
			Method:      http.MethodGet,
			Path:        "/terminals/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.GetTerminal,
			Summary:     "A terminal session",
			Response:    &dto.Response[models.TerminalSession]{},
		},
		{
			Method:      http.MethodGet,
			Path:        "/terminals/{id}/recording",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.TerminalRecording,
			Summary:     "Terminal recording (asciinema v2)",
		},
		{
			Method:      http.MethodGet,
			Path:        "/agents/{id}/terminal",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.Terminal,
			Summary:     "Open a recorded terminal (WebSocket)",
		},
		{
			Method:      http.MethodGet,
			Path:        "/miabi-watches",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.ListMiabiWatches,
			Summary:     "Watched Miabi apps",
			Response:    &dto.Response[[]models.MiabiWatch]{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/miabi-watches",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     okapi.H(r.h.CreateMiabiWatch),
			Summary:     "Watch a Miabi app: verify deploys, triage failures",
			Request:     &handlers.MiabiWatchRequest{},
		},
		{
			Method:      http.MethodPut,
			Path:        "/miabi-watches/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     okapi.H(r.h.UpdateMiabiWatch),
			Summary:     "Update a Miabi watch",
			Request:     &handlers.MiabiWatchRequest{},
		},
		{
			Method:      http.MethodDelete,
			Path:        "/miabi-watches/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.DeleteMiabiWatch,
			Summary:     "Stop watching a Miabi app",
		},
		{
			Method:  http.MethodPost,
			Path:    "/miabi/{id}",
			Group:   hooks,
			Handler: r.h.MiabiWebhook,
			Summary: "Miabi outbound webhook (X-Miabi-Signature HMAC)",
		},
		{
			Method:   http.MethodPost,
			Path:     "/alerts/{token}",
			Group:    hooks,
			Handler:  r.h.AlertWebhook,
			Summary:  "Alertmanager or generic alert webhook (secret URL)",
			Response: &dto.Response[handlers.AlertWebhookResult]{},
		},
	}
}
