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

func (r *Router) providerRoutes() []okapi.RouteDefinition {
	g := r.group("Model providers", "LLM endpoints behind the gateway. Keys never leave the control plane.")
	return []okapi.RouteDefinition{
		{
			Method:      http.MethodGet,
			Path:        "/providers",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.ListProviders,
			Summary:     "List providers",
			Response:    &dto.Response[[]models.ModelProvider]{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/providers",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     okapi.H(r.h.CreateProvider),
			Summary:     "Add a provider",
			Request:     &handlers.ProviderRequest{},
		},
		{
			Method:      http.MethodGet,
			Path:        "/providers/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.GetProvider,
			Summary:     "Get a provider, the agents using it and its last 30 days of usage",
			Response:    &dto.Response[handlers.ProviderDetail]{},
		},
		{
			Method:      http.MethodPut,
			Path:        "/providers/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     okapi.H(r.h.UpdateProvider),
			Summary:     "Update a provider",
			Request:     &handlers.ProviderRequest{},
		},
		{
			Method:      http.MethodDelete,
			Path:        "/providers/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.DeleteProvider,
			Summary:     "Delete a provider",
		},
		{
			Method:      http.MethodPost,
			Path:        "/providers/{id}/test",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.TestProvider,
			Summary:     "Send a test request",
			Response:    &dto.Response[handlers.TestResult]{},
		},
	}
}
