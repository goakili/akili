// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package license implements Akili's offline, signed commercial license token: an Ed25519-signed
// claims blob verified against a public key embedded in the binary, so verification works
// air-gapped. It is pure (no database, no build tag).
package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// tokenPrefix versions the wire format and is part of the signed message, so a token issued for
// one format, or for another product, can never be replayed under this one.
const tokenPrefix = "akili-v1"

var (
	// ErrMalformed is returned for a token that is not three dot-separated parts with the prefix.
	ErrMalformed = errors.New("license: malformed token")
	// ErrBadSignature is returned for a tampered or forged token.
	ErrBadSignature = errors.New("license: signature verification failed")
	// ErrBadKey is returned when a key is not a valid base64 Ed25519 key.
	ErrBadKey = errors.New("license: invalid key")
)

// Claims is the signed license payload. Limits use -1 for unlimited.
type Claims struct {
	LicenseID string `json:"license_id"`
	Customer  string `json:"customer"`
	Edition   string `json:"edition"`
	// InstallID binds the license to one deployment by its Install ID, the primary binding.
	InstallID string `json:"install_id,omitempty"`
	// URL optionally also binds it by the host of the deployment's public URL.
	URL       string         `json:"url,omitempty"`
	Flags     []string       `json:"flags"`
	Limits    map[string]int `json:"limits,omitempty"`
	NotBefore time.Time      `json:"not_before"`
	NotAfter  time.Time      `json:"not_after"`
	GraceDays int            `json:"grace_days"`
	IssuedAt  time.Time      `json:"issued_at"`
}

var b64 = base64.RawURLEncoding

// Sign returns a token of the form "akili-v1.<b64(claims)>.<b64(sig)>".
func Sign(privateKeyB64 string, c Claims) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(privateKeyB64))
	if err != nil || len(raw) != ed25519.PrivateKeySize {
		return "", ErrBadKey
	}
	payload, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	body := tokenPrefix + "." + b64.EncodeToString(payload)
	sig := ed25519.Sign(ed25519.PrivateKey(raw), []byte(body))
	return body + "." + b64.EncodeToString(sig), nil
}

// Verify checks the token's signature and returns its claims. It does not judge expiry: an
// expired but authentic token still parses, so the caller can apply the grace period.
func Verify(publicKeyB64, token string) (Claims, error) {
	var c Claims
	pubRaw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(publicKeyB64))
	if err != nil || len(pubRaw) != ed25519.PublicKeySize {
		return c, ErrBadKey
	}
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 || parts[0] != tokenPrefix {
		return c, ErrMalformed
	}
	payload, err := b64.DecodeString(parts[1])
	if err != nil {
		return c, ErrMalformed
	}
	sig, err := b64.DecodeString(parts[2])
	if err != nil {
		return c, ErrMalformed
	}
	if !ed25519.Verify(ed25519.PublicKey(pubRaw), []byte(parts[0]+"."+parts[1]), sig) {
		return c, ErrBadSignature
	}
	if err := json.Unmarshal(payload, &c); err != nil {
		return c, fmt.Errorf("license: bad claims: %w", err)
	}
	return c, nil
}

// State is where an installed license is in its life at a point in time.
type State string

const (
	StateValid    State = "valid"    // within term: full function
	StateGrace    State = "grace"    // past expiry, within grace days: full function and a warning
	StateDegraded State = "degraded" // past grace: features keep running but cannot be reconfigured
	StateNone     State = "none"     // no license, or not active yet: community
)

// Snapshot is a license resolved at a point in time.
type Snapshot struct {
	State     State
	Edition   string
	Customer  string
	LicenseID string
	InstallID string
	URL       string
	Flags     map[string]bool
	Limits    map[string]int
	NotAfter  time.Time
	GraceEnds time.Time
}

// Evaluate resolves claims at time now. A token whose NotBefore is in the future is not active.
func Evaluate(c Claims, now time.Time) Snapshot {
	flags := make(map[string]bool, len(c.Flags))
	for _, f := range c.Flags {
		flags[f] = true
	}
	s := Snapshot{Edition: c.Edition, Customer: c.Customer, LicenseID: c.LicenseID, InstallID: c.InstallID, URL: c.URL,
		Flags: flags, Limits: c.Limits, NotAfter: c.NotAfter, GraceEnds: c.NotAfter.AddDate(0, 0, c.GraceDays)}
	switch {
	case !c.NotBefore.IsZero() && now.Before(c.NotBefore):
		s.State = StateNone
	case now.Before(c.NotAfter):
		s.State = StateValid
	case now.Before(s.GraceEnds):
		s.State = StateGrace
	default:
		s.State = StateDegraded
	}
	return s
}
