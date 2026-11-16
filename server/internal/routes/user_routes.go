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
			Method:      http.MethodDelete,
			Path:        "/users/{id}/2fa",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.ResetUserTwoFactor,
			Summary:     "Reset a user's two-factor authentication (lost device)",
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
