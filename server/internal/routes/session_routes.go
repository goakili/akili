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

func (r *Router) sessionRoutes() []okapi.RouteDefinition {
	g := r.group("Sessions", "Live chat with agents and task transcripts.")
	return []okapi.RouteDefinition{
		{
			Method:      http.MethodGet,
			Path:        "/sessions",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.ListSessions,
			Summary:     "List sessions",
			Response:    &dto.Response[[]models.ChatSession]{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/sessions",
			Group:       g,
			Middlewares: r.guard(models.RoleOperator),
			Handler:     okapi.H(r.h.CreateSession),
			Summary:     "Start a chat with an agent",
			Request:     &handlers.SessionRequest{},
		},
		{
			Method:      http.MethodGet,
			Path:        "/sessions/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.GetSession,
			Summary:     "Session history and timeline",
			Response:    &dto.Response[handlers.SessionDetail]{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/sessions/{id}/messages",
			Group:       g,
			Middlewares: r.guard(models.RoleOperator),
			Handler:     okapi.H(r.h.PostMessage),
			Summary:     "Send a message to the agent",
			Request:     &handlers.MessageRequest{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/sessions/{id}/attachments",
			Group:       g,
			Middlewares: r.guard(models.RoleOperator),
			Handler:     r.h.UploadAttachment,
			Summary:     "Upload an image (PNG, JPEG, GIF or WebP, up to 5 MB) to send with a message",
			Response:    &dto.Response[models.Attachment]{},
		},
		{
			Method:      http.MethodGet,
			Path:        "/sessions/{id}/attachments/{attachment}",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.GetAttachment,
			Summary:     "Download an attached image",
		},
		{
			Method:      http.MethodPost,
			Path:        "/sessions/{id}/interrupt",
			Group:       g,
			Middlewares: r.guard(models.RoleOperator),
			Handler:     r.h.InterruptSession,
			Summary:     "Stop the agent's current turn",
		},
		{
			Method:      http.MethodPost,
			Path:        "/sessions/{id}/close",
			Group:       g,
			Middlewares: r.guard(models.RoleOperator),
			Handler:     r.h.CloseSession,
			Summary:     "Close a session",
		},
		{
			Method:      http.MethodGet,
			Path:        "/sessions/{id}/stream",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.StreamSession,
			Summary:     "Live session events (SSE)",
		},
		{
			Method:      http.MethodGet,
			Path:        "/events/stream",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.StreamEvents,
			Summary:     "Live organization events (SSE)",
		},
	}
}
