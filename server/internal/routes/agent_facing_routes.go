// SPDX-FileCopyrightText: 2026 Jonas Kaninda
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
