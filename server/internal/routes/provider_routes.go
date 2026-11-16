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
