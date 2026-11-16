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

func (r *Router) chatRoutes() []okapi.RouteDefinition {
	g := r.group("Chat", "Slack, Telegram and Signal gateways to agents.")
	return []okapi.RouteDefinition{
		{
			Method:      http.MethodGet,
			Path:        "/chat/channels",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.ListChatChannels,
			Summary:     "List chat channels",
			Response:    &dto.Response[[]handlers.ChatChannelView]{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/chat/channels",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     okapi.H(r.h.CreateChatChannel),
			Summary:     "Add a chat channel (Slack app, Telegram bot, Signal number)",
			Request:     &handlers.ChatChannelRequest{},
			Response:    &dto.Response[handlers.ChatChannelView]{},
		},
		{
			Method:      http.MethodPut,
			Path:        "/chat/channels/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     okapi.H(r.h.UpdateChatChannel),
			Summary:     "Update a chat channel",
			Request:     &handlers.ChatChannelRequest{},
		},
		{
			Method:      http.MethodDelete,
			Path:        "/chat/channels/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.DeleteChatChannel,
			Summary:     "Delete a chat channel",
		},
		{
			Method:      http.MethodPost,
			Path:        "/chat/channels/{id}/test",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.TestChatChannel,
			Summary:     "Check a channel's credentials",
			Response:    &dto.Response[handlers.TestResult]{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/chat/link-code",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.CreateChatLinkCode,
			Summary:     "One-time code to link your chat account (send /link <code> to the bot)",
			Response:    &dto.Response[handlers.ChatLinkCode]{},
		},
		{
			Method:      http.MethodGet,
			Path:        "/chat/identities",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.ListChatIdentities,
			Summary:     "Linked chat accounts (admins: all)",
			Response:    &dto.Response[[]models.ChatIdentity]{},
		},
		{
			Method:      http.MethodDelete,
			Path:        "/chat/identities/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.DeleteChatIdentity,
			Summary:     "Unlink a chat account",
		},
		{
			Method:  http.MethodPost,
			Path:    "/chat/slack/{id}/events",
			Group:   g,
			Handler: r.h.SlackEvents,
			Summary: "Slack Events API (signed)",
		},
		{
			Method:  http.MethodPost,
			Path:    "/chat/slack/{id}/interact",
			Group:   g,
			Handler: r.h.SlackInteract,
			Summary: "Slack interactivity (signed)",
		},
	}
}
