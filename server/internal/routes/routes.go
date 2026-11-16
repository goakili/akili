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

// Package routes declares every HTTP route as data, so its auth, role and OpenAPI docs live together.
package routes

import (
	"fmt"
	"io/fs"
	"net/http"
	"time"

	"github.com/goakili/akili/server/internal/config"
	"github.com/goakili/akili/server/internal/handlers"
	"github.com/goakili/akili/server/internal/middlewares"
	"github.com/goakili/akili/server/internal/storage/pagination"
	"github.com/goakili/akili/server/internal/web"
	"github.com/jkaninda/okapi"
)

type Router struct {
	app   *okapi.Okapi
	h     *handlers.Handlers
	authn *middlewares.Authenticator
	v1    *okapi.Group
}

// Register mounts every route, then the web UI last so API routes win.
func Register(app *okapi.Okapi, h *handlers.Handlers, authn *middlewares.Authenticator) {
	r := &Router{app: app, h: h, authn: authn, v1: app.Group("/api/v1")}
	app.NoRoute(func(c *okapi.Context) error { return c.AbortNotFound("route not found") })
	app.NoMethod(func(c *okapi.Context) error { return c.AbortMethodNotAllowed("method not allowed") })

	for _, defs := range [][]okapi.RouteDefinition{
		r.healthRoutes(), r.agentFacingRoutes(), r.authRoutes(), r.userRoutes(), r.agentRoutes(), r.sessionRoutes(),
		r.approvalRoutes(), r.questionRoutes(), r.taskRoutes(), r.scheduleRoutes(), r.skillRoutes(), r.policyRoutes(), r.providerRoutes(),
		r.systemRoutes(), r.projectRoutes(), r.opsRoutes(), r.chatRoutes(), r.lessonRoutes(), r.planRoutes(), r.mcpRoutes(), r.autonomyRoutes(), r.reviewRoutes(),
	} {
		app.Register(defs...)
	}

	installScript := web.InstallScript(config.AgentVersion())
	app.Get("/install-agent.sh", func(c *okapi.Context) error {
		c.SetHeader("Cache-Control", "no-cache")
		return c.Data(http.StatusOK, "text/x-shellscript; charset=utf-8", installScript)
	}, okapi.DocHide())

	if h.Cfg.WebDir != "" {
		app.Web("/", h.Cfg.WebDir)
	} else if sub, err := fs.Sub(web.Assets, "dist"); err == nil {
		if _, err := fs.Stat(sub, "index.html"); err == nil {
			app.WebFS("/", web.Assets, okapi.WebConfig{Root: "dist", MaxAge: time.Hour})
		}
	}
}

func (r *Router) guard(min string) []okapi.Middleware {
	return []okapi.Middleware{r.authn.Authenticate, middlewares.RequireRole(min)}
}

func (r *Router) group(name, desc string) *okapi.Group {
	g := r.v1.Group("").WithTagInfo(okapi.GroupTag{Name: name, Description: desc})
	g.WithBearerAuth()
	return g
}

func pageDocs() []okapi.RouteOption {
	return []okapi.RouteOption{
		okapi.DocQueryParamWithDefault("page", "integer", "0-based page; the response has a pageable block while a next page exists", false, 0),
		okapi.DocQueryParamWithDefault("size", "integer", fmt.Sprintf("rows per page, at most %d", pagination.MaxSize), false, pagination.DefaultSize),
	}
}
