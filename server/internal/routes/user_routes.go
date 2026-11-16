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
