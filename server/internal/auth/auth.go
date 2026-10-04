// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package auth handles operator login, sessions (JWT with Redis revocation), API keys and users.
package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/goakili/akili/server/internal/crypto"
	"github.com/goakili/akili/server/internal/models"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jkaninda/okapi"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// Audience of operator session tokens.
const Audience = "akili"

// Service is the auth service.
type Service struct {
	db     *gorm.DB
	rdb    *redis.Client
	box    *crypto.Box
	secret []byte
	ttl    time.Duration
	// PasswordOwnerOnly limits password login to the owner (break-glass) when SSO is mandatory.
	PasswordOwnerOnly bool
}

// New returns the auth service. box seals TOTP secrets.
func New(db *gorm.DB, rdb *redis.Client, box *crypto.Box, secret string, ttl time.Duration) *Service {
	return &Service{db: db, rdb: rdb, box: box, secret: []byte(secret), ttl: ttl}
}

// Errors.
var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrNotFound           = errors.New("user not found")
)

// dummyHash keeps login timing equal for unknown and known emails.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("akili-timing-equaliser"), bcrypt.DefaultCost)

// CheckPassword verifies credentials without starting a session; a user with two-factor
// authentication still needs StartMFA and CompleteMFA.
func (s *Service) CheckPassword(ctx context.Context, email, password string) (*models.User, error) {
	var u models.User
	err := s.db.WithContext(ctx).First(&u, "lower(email) = ?", strings.ToLower(strings.TrimSpace(email))).Error
	if err != nil {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return nil, ErrInvalidCredentials
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil || !u.Active {
		return nil, ErrInvalidCredentials
	}
	if s.PasswordOwnerOnly && u.Role != models.RoleOwner {
		return nil, ErrInvalidCredentials
	}
	return &u, nil
}

// Issue starts a session for an authenticated user (password or SSO).
func (s *Service) Issue(ctx context.Context, u *models.User) (string, time.Time, error) {
	now := time.Now().UTC()
	s.db.WithContext(ctx).Model(u).Update("last_login_at", now)
	exp := now.Add(s.ttl)
	token, err := okapi.GenerateJwtToken(s.secret, jwt.MapClaims{
		"sub": u.ID, "org": u.OrganizationID, "role": u.Role, "email": u.Email, "aud": Audience,
		"jti": models.NewID("jti"), "iat": now.Unix(),
	}, s.ttl)
	return token, exp, err
}

// Revoke invalidates a session token id until it would have expired anyway.
func (s *Service) Revoke(ctx context.Context, jti string, exp time.Time) {
	ttl := time.Until(exp)
	if jti == "" || ttl <= 0 {
		return
	}
	s.rdb.Set(ctx, "akili:revoked:"+jti, 1, ttl)
}

// IsRevoked reports whether a token id was revoked (fails closed when Redis is unavailable).
func (s *Service) IsRevoked(ctx context.Context, jti string) bool {
	n, err := s.rdb.Exists(ctx, "akili:revoked:"+jti).Result()
	return err != nil || n > 0
}

// Secret is the JWT signing secret (for the middleware).
func (s *Service) Secret() []byte { return s.secret }

// TTL is the session lifetime.
func (s *Service) TTL() time.Duration { return s.ttl }

// User loads a user within an organization.
func (s *Service) User(ctx context.Context, org, id string) (*models.User, error) {
	var u models.User
	if err := s.db.WithContext(ctx).First(&u, "id = ? AND organization_id = ?", id, org).Error; err != nil {
		return nil, ErrNotFound
	}
	return &u, nil
}

// ---- API keys ------------------------------------------------------------------------------------

// APIKeyPrefix marks Akili API keys.
const APIKeyPrefix = "ak_"

// CreateAPIKey issues a key for a user. The secret is returned once.
func (s *Service) CreateAPIKey(ctx context.Context, u *models.User, name string, scopes []string, expires *time.Time) (*models.APIKey, string, error) {
	if strings.TrimSpace(name) == "" {
		return nil, "", errors.New("name is required")
	}
	if len(scopes) == 0 {
		scopes = []string{models.ScopeRead}
	}
	for _, sc := range scopes {
		if sc != models.ScopeRead && sc != models.ScopeWrite {
			return nil, "", errors.New("scopes must be read and/or write")
		}
	}
	secret := crypto.NewToken("ak")
	k := &models.APIKey{Base: models.Base{ID: models.NewID("key"), OrganizationID: u.OrganizationID}, UserID: u.ID, Name: name,
		Prefix: secret[:10], Hash: crypto.HashToken(secret), Scopes: scopes, ExpiresAt: expires}
	if err := s.db.WithContext(ctx).Create(k).Error; err != nil {
		return nil, "", err
	}
	return k, secret, nil
}

// VerifyAPIKey resolves a presented key to its user.
func (s *Service) VerifyAPIKey(ctx context.Context, raw string) (*models.APIKey, *models.User, error) {
	var k models.APIKey
	if err := s.db.WithContext(ctx).First(&k, "hash = ?", crypto.HashToken(raw)).Error; err != nil {
		return nil, nil, errors.New("invalid API key")
	}
	now := time.Now().UTC()
	if k.RevokedAt != nil || (k.ExpiresAt != nil && now.After(*k.ExpiresAt)) {
		return nil, nil, errors.New("API key is revoked or expired")
	}
	var u models.User
	if err := s.db.WithContext(ctx).First(&u, "id = ?", k.UserID).Error; err != nil || !u.Active {
		return nil, nil, errors.New("API key owner is disabled")
	}
	if k.LastUsedAt == nil || now.Sub(*k.LastUsedAt) > time.Minute {
		s.db.WithContext(ctx).Model(&k).Update("last_used_at", now)
	}
	return &k, &u, nil
}

// HashPassword hashes a new password after checking its length.
func HashPassword(p string) (string, error) {
	if len(p) < 12 {
		return "", errors.New("password must be at least 12 characters")
	}
	if len(p) > 72 {
		return "", errors.New("password must be at most 72 characters")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(p), bcrypt.DefaultCost)
	return string(h), err
}

// FailureBlocked reports whether key has reached n recorded failures in its window. Used where
// successes are authenticated and must not count (a fleet behind one NAT reconnecting at once).
func FailureBlocked(ctx context.Context, rdb *redis.Client, key string, n int) bool {
	cnt, err := rdb.Get(ctx, "akili:fail:"+key).Int64()
	return err == nil && cnt >= int64(n)
}

// RecordFailure counts a failed attempt for key.
func RecordFailure(ctx context.Context, rdb *redis.Client, key string, window time.Duration) {
	k := "akili:fail:" + key
	if cnt, err := rdb.Incr(ctx, k).Result(); err == nil && cnt == 1 {
		rdb.Expire(ctx, k, window)
	}
}

// RateLimit allows n hits per window for key; it fails open only when Redis is down for non-login
// callers (failClosed=false).
func RateLimit(ctx context.Context, rdb *redis.Client, key string, n int, window time.Duration, failClosed bool) bool {
	k := "akili:rl:" + key
	cnt, err := rdb.Incr(ctx, k).Result()
	if err != nil {
		return !failClosed
	}
	if cnt == 1 {
		rdb.Expire(ctx, k, window)
	}
	return cnt <= int64(n)
}
