// Copyright (c) 2026 Jonas Kaninda.
//
// This file is part of Akili.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package routes

import (
	"net/http"

	"github.com/goakili/akili/server/internal/dto"
	"github.com/goakili/akili/server/internal/handlers"
	"github.com/goakili/akili/server/internal/models"
	"github.com/jkaninda/okapi"
)

func (r *Router) licenseRoutes() []okapi.RouteDefinition {
	g := r.group("License", "The edition and the Akili Enterprise license.")
	return []okapi.RouteDefinition{
		{
			Method:      http.MethodGet,
			Path:        "/license",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.GetLicense,
			Summary:     "Edition, installed license and the features it grants",
			Response:    &dto.Response[handlers.LicenseView]{},
		},
		{
			Method:      http.MethodPut,
			Path:        "/license",
			Group:       g,
			Middlewares: r.guard(models.RoleOwner),
			Handler:     okapi.H(r.h.InstallLicense),
			Summary:     "Install an Enterprise license, replacing any other",
			Request:     &handlers.LicenseRequest{},
			Response:    &dto.Response[handlers.LicenseView]{},
		},
		{
			Method:      http.MethodDelete,
			Path:        "/license",
			Group:       g,
			Middlewares: r.guard(models.RoleOwner),
			Handler:     r.h.RemoveLicense,
			Summary:     "Remove the license and continue as Community",
			Response:    &dto.Response[handlers.LicenseView]{},
		},
	}
}
