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
