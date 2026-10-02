// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package oidctest is a minimal OpenID Connect provider for tests: discovery, JWKS, an authorize
// endpoint that signs in a preset user without a login page, and a token endpoint that checks the
// client secret and PKCE and issues RS256 ID tokens.
package oidctest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// User is who the provider signs in.
type User struct {
	Subject       string   `json:"sub"`
	Email         string   `json:"email"`
	EmailVerified bool     `json:"email_verified"`
	Name          string   `json:"name"`
	Groups        []string `json:"groups"`
}

// Provider is the fake identity provider.
type Provider struct {
	Issuer       string
	ClientID     string
	ClientSecret string

	key  *rsa.PrivateKey
	mu   sync.Mutex
	user User
	code map[string]grant
}

type grant struct {
	nonce, challenge, redirect string
	user                       User
}

// New returns a provider; set Issuer to the URL it is served at.
func New(clientID, clientSecret string) *Provider {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	return &Provider{ClientID: clientID, ClientSecret: clientSecret, key: key, code: map[string]grant{},
		user: User{Subject: "u-1", Email: "sso-user@example.com", EmailVerified: true, Name: "SSO User"}}
}

// SetUser changes who signs in next.
func (p *Provider) SetUser(u User) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.user = u
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// ServeHTTP implements the provider endpoints plus POST /_fake/user to set the next user.
func (p *Provider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		writeJSON(w, 200, map[string]any{
			"issuer": p.Issuer, "authorization_endpoint": p.Issuer + "/authorize", "token_endpoint": p.Issuer + "/token",
			"jwks_uri": p.Issuer + "/jwks", "response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"}, "code_challenge_methods_supported": []string{"S256"},
		})
	case "/jwks":
		writeJSON(w, 200, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &p.key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	case "/authorize":
		q := r.URL.Query()
		if q.Get("client_id") != p.ClientID || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" {
			http.Error(w, "bad authorization request", http.StatusBadRequest)
			return
		}
		code := rand.Text()
		p.mu.Lock()
		p.code[code] = grant{nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri"), user: p.user}
		p.mu.Unlock()
		u, _ := url.Parse(q.Get("redirect_uri"))
		v := u.Query()
		v.Set("code", code)
		v.Set("state", q.Get("state"))
		u.RawQuery = v.Encode()
		http.Redirect(w, r, u.String(), http.StatusFound)
	case "/token":
		_ = r.ParseForm()
		id, secret, ok := r.BasicAuth()
		if !ok {
			id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
		}
		if id != p.ClientID || secret != p.ClientSecret {
			writeJSON(w, 401, map[string]string{"error": "invalid_client"})
			return
		}
		p.mu.Lock()
		g, found := p.code[r.PostForm.Get("code")]
		delete(p.code, r.PostForm.Get("code"))
		p.mu.Unlock()
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if !found || g.redirect != r.PostForm.Get("redirect_uri") || base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
			writeJSON(w, 400, map[string]string{"error": "invalid_grant"})
			return
		}
		signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: p.key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k1"))
		now := time.Now()
		claims := map[string]any{"iss": p.Issuer, "aud": p.ClientID, "sub": g.user.Subject, "email": g.user.Email,
			"email_verified": g.user.EmailVerified, "name": g.user.Name, "groups": g.user.Groups, "nonce": g.nonce,
			"iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix()}
		tok, err := jwt.Signed(signer).Claims(claims).Serialize()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"access_token": rand.Text(), "token_type": "Bearer", "expires_in": 300, "id_token": tok})
	case "/_fake/user":
		var u User
		if json.NewDecoder(r.Body).Decode(&u) != nil {
			http.Error(w, "bad user", 400)
			return
		}
		p.SetUser(u)
		writeJSON(w, 200, u)
	default:
		http.NotFound(w, r)
	}
}
