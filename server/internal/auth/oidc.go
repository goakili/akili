// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/goakili/akili/server/internal/config"
	"github.com/goakili/akili/server/internal/models"
	"github.com/redis/go-redis/v9"
	"golang.org/x/oauth2"
	"gorm.io/gorm"
)

// OIDC signs operators in with an OpenID Connect provider (authorization code flow with PKCE).
type OIDC struct {
	cfg      config.OIDCConfig
	redirect string
	db       *gorm.DB
	rdb      *redis.Client
	auth     *Service

	mu       sync.Mutex
	provider *oidc.Provider
}

// ErrSSODenied is returned when the identity provider vouched for someone Akili does not admit.
var ErrSSODenied = errors.New("single sign-on refused")

// NewOIDC returns the SSO flow. Discovery is lazy so an identity-provider outage does not stop the
// control plane from starting (password break-glass keeps working).
func NewOIDC(cfg config.OIDCConfig, publicURL string, db *gorm.DB, rdb *redis.Client, auth *Service) *OIDC {
	return &OIDC{cfg: cfg, redirect: strings.TrimRight(publicURL, "/") + "/api/v1/auth/oidc/callback", db: db, rdb: rdb, auth: auth}
}

// Name is the login button label.
func (o *OIDC) Name() string { return o.cfg.Name }

func (o *OIDC) discover(ctx context.Context) (*oidc.Provider, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.provider != nil {
		return o.provider, nil
	}
	p, err := oidc.NewProvider(ctx, o.cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("identity provider discovery: %w", err)
	}
	o.provider = p
	return p, nil
}

func (o *OIDC) oauth(p *oidc.Provider) *oauth2.Config {
	return &oauth2.Config{ClientID: o.cfg.ClientID, ClientSecret: o.cfg.ClientSecret, Endpoint: p.Endpoint(),
		RedirectURL: o.redirect, Scopes: o.cfg.Scopes}
}

type pending struct {
	Nonce    string `json:"nonce"`
	Verifier string `json:"verifier"`
}

func stateKey(state string) string { return "akili:oidc:state:" + state }

// Begin starts a login: it returns the provider URL and the state the browser must bring back.
func (o *OIDC) Begin(ctx context.Context) (string, string, error) {
	p, err := o.discover(ctx)
	if err != nil {
		return "", "", err
	}
	state, nonce, verifier := models.NewID("st"), models.NewID("nc"), oauth2.GenerateVerifier()
	b, _ := json.Marshal(pending{Nonce: nonce, Verifier: verifier})
	if err := o.rdb.Set(ctx, stateKey(state), b, 10*time.Minute).Err(); err != nil {
		return "", "", err
	}
	return o.oauth(p).AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), state, nil
}

type claims struct {
	Subject       string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified *bool  `json:"email_verified"`
	Name          string `json:"name"`
}

// Finish exchanges the code, verifies the ID token and returns the signed-in user with a session.
// The state is single-use.
func (o *OIDC) Finish(ctx context.Context, state, code string) (*models.User, string, time.Time, error) {
	raw, err := o.rdb.GetDel(ctx, stateKey(state)).Bytes()
	if err != nil {
		return nil, "", time.Time{}, fmt.Errorf("%w: unknown or expired login state", ErrSSODenied)
	}
	var pd pending
	if err := json.Unmarshal(raw, &pd); err != nil {
		return nil, "", time.Time{}, err
	}
	p, err := o.discover(ctx)
	if err != nil {
		return nil, "", time.Time{}, err
	}
	tok, err := o.oauth(p).Exchange(ctx, code, oauth2.VerifierOption(pd.Verifier))
	if err != nil {
		return nil, "", time.Time{}, fmt.Errorf("%w: code exchange failed: %v", ErrSSODenied, err)
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok {
		return nil, "", time.Time{}, fmt.Errorf("%w: no id_token in the token response", ErrSSODenied)
	}
	idt, err := p.Verifier(&oidc.Config{ClientID: o.cfg.ClientID}).Verify(ctx, rawID)
	if err != nil {
		return nil, "", time.Time{}, fmt.Errorf("%w: %v", ErrSSODenied, err)
	}
	if idt.Nonce != pd.Nonce {
		return nil, "", time.Time{}, fmt.Errorf("%w: nonce mismatch", ErrSSODenied)
	}
	var c claims
	var all map[string]any
	if err := idt.Claims(&c); err != nil {
		return nil, "", time.Time{}, err
	}
	_ = idt.Claims(&all)
	u, err := o.admit(ctx, idt.Issuer+"|"+c.Subject, c, groups(all[o.cfg.RoleClaim]))
	if err != nil {
		return nil, "", time.Time{}, err
	}
	token, exp, err := o.auth.Issue(ctx, u)
	return u, token, exp, err
}

func groups(v any) []string {
	switch g := v.(type) {
	case []any:
		out := make([]string, 0, len(g))
		for _, x := range g {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		return strings.Fields(strings.ReplaceAll(g, ",", " "))
	}
	return nil
}

// mappedRole returns the highest role granted by the user's groups, or "" when none matches.
func (o *OIDC) mappedRole(gs []string) string {
	best := ""
	for _, g := range gs {
		if r, ok := o.cfg.RoleMap[g]; ok && models.RoleRank(r) > models.RoleRank(best) {
			best = r
		}
	}
	return best
}

// admit links the identity to a user, creating one on first sign-in.
func (o *OIDC) admit(ctx context.Context, subject string, c claims, gs []string) (*models.User, error) {
	email := strings.ToLower(strings.TrimSpace(c.Email))
	if email == "" || c.Subject == "" {
		return nil, fmt.Errorf("%w: the identity provider sent no email", ErrSSODenied)
	}
	// Linking by email is only safe when the provider vouches for the address.
	if c.EmailVerified == nil || !*c.EmailVerified {
		return nil, fmt.Errorf("%w: the email address is not verified by the identity provider", ErrSSODenied)
	}
	if len(o.cfg.AllowedDomains) > 0 {
		_, domain, _ := strings.Cut(email, "@")
		if !slices.Contains(o.cfg.AllowedDomains, domain) {
			return nil, fmt.Errorf("%w: %s is not an allowed domain", ErrSSODenied, domain)
		}
	}
	role := o.mappedRole(gs)
	if len(o.cfg.RoleMap) > 0 && role == "" && o.cfg.DefaultRole == "" {
		return nil, fmt.Errorf("%w: no group grants access", ErrSSODenied)
	}
	var u models.User
	err := o.db.WithContext(ctx).Where("sso_subject = ?", subject).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = o.db.WithContext(ctx).Where("lower(email) = ?", email).First(&u).Error
		if err == nil && u.SSOSubject != "" && u.SSOSubject != subject {
			return nil, fmt.Errorf("%w: this email is linked to another identity", ErrSSODenied)
		}
	}
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		var org models.Organization
		if err := o.db.WithContext(ctx).Order("created_at").First(&org).Error; err != nil {
			return nil, err
		}
		if role == "" {
			role = o.cfg.DefaultRole
		}
		name := c.Name
		if name == "" {
			name = email
		}
		u = models.User{Base: models.Base{ID: models.NewID("usr"), OrganizationID: org.ID}, Email: email, Name: name, Role: role,
			Active: true, SSOSubject: subject}
		if err := o.db.WithContext(ctx).Create(&u).Error; err != nil {
			return nil, err
		}
		return &u, nil
	case err != nil:
		return nil, err
	}
	if !u.Active {
		return nil, fmt.Errorf("%w: the account is deactivated", ErrSSODenied)
	}
	updates := map[string]any{"sso_subject": subject}
	// Group membership is authoritative for roles, except that SSO never grants or removes owner.
	if role != "" && u.Role != models.RoleOwner && role != u.Role {
		updates["role"] = role
		u.Role = role
	}
	u.SSOSubject = subject
	if err := o.db.WithContext(ctx).Model(&u).Updates(updates).Error; err != nil {
		return nil, err
	}
	return &u, nil
}
