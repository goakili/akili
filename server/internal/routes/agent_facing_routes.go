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

	"github.com/jkaninda/okapi"
)

// agentFacingRoutes are called by agents, not operators. They carry their own authentication (a
// one-time join token, or a signed handshake).
func (r *Router) agentFacingRoutes() []okapi.RouteDefinition {
	g := r.v1.Group("/agent").WithTagInfo(okapi.GroupTag{Name: "Agent protocol", Description: "Endpoints used by akili-agent."})
	return []okapi.RouteDefinition{
		{
			Method:  http.MethodPost,
			Path:    "/enroll",
			Group:   g,
			Handler: r.h.Enroll,
			Summary: "Enroll an agent with a one-time join token",
		},
		{
			Method:  http.MethodGet,
			Path:    "/connect",
			Group:   g,
			Handler: r.h.Connect,
			Summary: "Open the agent tunnel (WebSocket, signed handshake)",
		},
	}
}
