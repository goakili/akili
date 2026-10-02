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

func (r *Router) userRoutes() []okapi.RouteDefinition {
	g := r.group("Users", "Operators and API keys.")
	return []okapi.RouteDefinition{
		{
			Method:      http.MethodGet,
			Path:        "/users",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.ListUsers,
			Summary:     "List users",
			Response:    &dto.Response[[]models.User]{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/users",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     okapi.H(r.h.CreateUser),
			Summary:     "Create a user",
			Request:     &handlers.UserRequest{},
		},
		{
			Method:      http.MethodPatch,
			Path:        "/users/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     okapi.H(r.h.UpdateUser),
			Summary:     "Update a user",
			Request:     &handlers.UserRequest{},
		},
		{
			Method:      http.MethodGet,
			Path:        "/api-keys",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.ListAPIKeys,
			Summary:     "List my API keys",
			Response:    &dto.Response[[]models.APIKey]{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/api-keys",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     okapi.H(r.h.CreateAPIKey),
			Summary:     "Create an API key (secret shown once)",
			Request:     &handlers.APIKeyRequest{},
			Response:    &dto.Response[handlers.APIKeyCreated]{},
		},
		{
			Method:      http.MethodDelete,
			Path:        "/api-keys/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.RevokeAPIKey,
			Summary:     "Revoke an API key",
		},
	}
}
