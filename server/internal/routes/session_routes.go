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
			Response:    &dto.PageResponse[models.ChatSession]{},
			Options:     pageDocs(),
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
