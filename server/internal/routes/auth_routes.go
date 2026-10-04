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

func (r *Router) authRoutes() []okapi.RouteDefinition {
	g := r.v1.Group("/auth").WithTagInfo(okapi.GroupTag{Name: "Auth"})
	return []okapi.RouteDefinition{
		{
			Method:   http.MethodPost,
			Path:     "/login",
			Group:    g,
			Handler:  okapi.H(r.h.Login),
			Summary:  "Log in",
			Request:  &handlers.LoginRequest{},
			Response: &dto.Response[handlers.LoginResponse]{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/logout",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.Logout,
			Summary:     "Log out (revokes the session)",
		},
		{
			Method:      http.MethodGet,
			Path:        "/me",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.Me,
			Summary:     "Current operator",
			Response:    &dto.Response[handlers.MeResponse]{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/password",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     okapi.H(r.h.ChangePassword),
			Summary:     "Change own password",
			Request:     &handlers.ChangePasswordRequest{},
		},
		{
			Method:      http.MethodGet,
			Path:        "/notifications",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.GetNotifications,
			Summary:     "Own email notification settings",
			Response:    &dto.Response[handlers.NotificationSettings]{},
		},
		{
			Method:      http.MethodPut,
			Path:        "/notifications",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     okapi.H(r.h.UpdateNotifications),
			Summary:     "Change own email notification settings",
			Request:     &handlers.NotificationsRequest{},
			Response:    &dto.Response[handlers.NotificationSettings]{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/notifications/test",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.TestNotification,
			Summary:     "Send yourself a test email through Posta",
		},
		{
			Method:      http.MethodGet,
			Path:        "/git-identity",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     r.h.GetGitIdentity,
			Summary:     "How you are credited on agent commits and pull requests",
			Response:    &dto.Response[handlers.GitIdentitySettings]{},
		},
		{
			Method:      http.MethodPut,
			Path:        "/git-identity",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     okapi.H(r.h.UpdateGitIdentity),
			Summary:     "Change how you are credited on agent commits and pull requests",
			Request:     &handlers.GitIdentityRequest{},
			Response:    &dto.Response[handlers.GitIdentitySettings]{},
		},
		{
			Method:      http.MethodPost,
			Path:        "/vscode/authorize",
			Group:       g,
			Middlewares: r.guard(models.RoleViewer),
			Handler:     okapi.H(r.h.VSCodeAuthorize),
			Summary:     "Approve an editor sign-in (browser session only); returns the editor redirect with a one-time code",
			Request:     &handlers.VSCodeAuthorizeRequest{},
			Response:    &dto.Response[handlers.VSCodeAuthorized]{},
		},
		{
			Method:   http.MethodPost,
			Path:     "/vscode/token",
			Group:    g,
			Handler:  okapi.H(r.h.VSCodeToken),
			Summary:  "Exchange a one-time editor code and its PKCE verifier for an API key",
			Request:  &handlers.VSCodeTokenRequest{},
			Response: &dto.Response[handlers.VSCodeToken]{},
		},
		{
			Method:   http.MethodGet,
			Path:     "/providers",
			Group:    g,
			Handler:  r.h.Providers,
			Summary:  "Sign-in methods (password, SSO)",
			Response: &dto.Response[handlers.AuthProviders]{},
		},
		{
			Method:  http.MethodGet,
			Path:    "/oidc/login",
			Group:   g,
			Handler: r.h.OIDCLogin,
			Summary: "Start single sign-on (redirects to the identity provider)",
		},
		{
			Method:  http.MethodGet,
			Path:    "/oidc/callback",
			Group:   g,
			Handler: r.h.OIDCCallback,
			Summary: "Single sign-on callback",
		},
	}
}
