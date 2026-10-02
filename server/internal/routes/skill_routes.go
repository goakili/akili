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
