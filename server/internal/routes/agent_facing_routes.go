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

	"github.com/jkaninda/okapi"
)

// agentFacingRoutes carry their own authentication (a one-time join token or a signed handshake).
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
