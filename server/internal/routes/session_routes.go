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
