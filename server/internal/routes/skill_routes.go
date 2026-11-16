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

func (r *Router) skillRoutes() []okapi.RouteDefinition {
	g := r.group("Skills", "Procedures provisioned to agents.")
	return []okapi.RouteDefinition{
		{
			Method:      http.MethodGet,
			Path:        "/skills",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.ListSkills,
			Summary:     "List skills",
			Response:    &dto.Response[[]models.Skill]{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/skills",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     okapi.H(r.h.CreateSkill),
			Summary:     "Create a skill",
			Request:     &handlers.SkillRequest{},
		},
		{
			Method:      http.MethodGet,
			Path:        "/skills/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.GetSkill,
			Summary:     "Get a skill",
			Response:    &dto.Response[models.Skill]{},
		},
		{
			Method:      http.MethodPut,
			Path:        "/skills/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     okapi.H(r.h.UpdateSkill),
			Summary:     "Update a skill",
			Request:     &handlers.SkillRequest{},
		},
		{
			Method:      http.MethodDelete,
			Path:        "/skills/{id}",
			Group:       g,
			Middlewares: r.guard(models.RoleAdmin),
			Handler:     r.h.DeleteSkill,
			Summary:     "Delete a skill",
		},
	}
}
