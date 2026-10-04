// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package routes declares every HTTP route as data (okapi.RouteDefinition), so each route's auth,
// role and OpenAPI documentation live together.
package routes

import (
	"io/fs"
	"net/http"
	"time"

	"github.com/goakili/akili/server/internal/config"
	"github.com/goakili/akili/server/internal/handlers"
	"github.com/goakili/akili/server/internal/middlewares"
	"github.com/goakili/akili/server/internal/web"
	"github.com/jkaninda/okapi"
)

// Router wires handlers to routes.
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
		r.approvalRoutes(), r.taskRoutes(), r.scheduleRoutes(), r.skillRoutes(), r.policyRoutes(), r.providerRoutes(),
		r.systemRoutes(), r.projectRoutes(), r.opsRoutes(), r.chatRoutes(), r.lessonRoutes(), r.planRoutes(), r.mcpRoutes(), r.licenseRoutes(),
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

// guard returns the middleware chain for an authenticated route at a minimum role.
func (r *Router) guard(min string) []okapi.Middleware {
	return []okapi.Middleware{r.authn.Authenticate, middlewares.RequireRole(min)}
}

func (r *Router) group(name, desc string) *okapi.Group {
	g := r.v1.Group("").WithTagInfo(okapi.GroupTag{Name: name, Description: desc})
	g.WithBearerAuth()
	return g
}
