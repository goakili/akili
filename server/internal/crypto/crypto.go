// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package crypto holds at-rest encryption, token hashing and the policy-signing key.
package crypto

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Box encrypts secrets at rest with AES-256-GCM under the keyring's active data key.
type Box struct {
	ring *Keyring
}

// NewBox returns a box over a keyring.
func NewBox(ring *Keyring) *Box { return &Box{ring: ring} }

// Keyring exposes the key management behind the box.
func (b *Box) Keyring() *Keyring { return b.ring }

// boxPrefix marks ciphertexts written before envelope encryption.
const boxPrefix = "v1:"

// Encrypt returns "v2:<key id>:<base64(nonce|ciphertext)>". Empty input stays empty.
func (b *Box) Encrypt(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	return b.ring.encrypt(plain)
}

// Decrypt reverses Encrypt; it also reads v1 ciphertexts.
func (b *Box) Decrypt(enc string) (string, error) {
	if enc == "" {
		return "", nil
	}
	return b.ring.decrypt(enc)
}

// Reencrypt returns enc sealed under the active data key, or enc unchanged when it already is.
func (b *Box) Reencrypt(enc string) (string, bool, error) {
	if enc == "" || KeyID(enc) == b.ring.Active() {
		return enc, false, nil
	}
	plain, err := b.Decrypt(enc)
	if err != nil {
		return "", false, err
	}
	out, err := b.Encrypt(plain)
	return out, err == nil, err
}

// MemoryStore is an in-memory KeyStore for tests and tools.
type MemoryStore struct {
	mu sync.Mutex
	m  map[string]string
}

// NewMemoryStore returns an empty store.
func NewMemoryStore() *MemoryStore { return &MemoryStore{m: map[string]string{}} }

// Get implements KeyStore.
func (s *MemoryStore) Get(_ context.Context, key string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[key]
	return v, ok, nil
}

// Create implements KeyStore.
func (s *MemoryStore) Create(_ context.Context, key, value string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[key]; ok {
		return false, nil
	}
	s.m[key] = value
	return true, nil
}

// Put implements KeyStore.
func (s *MemoryStore) Put(_ context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = value
	return nil
}

// Keys implements KeyStore.
func (s *MemoryStore) Keys(_ context.Context, prefix string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for k := range s.m {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out, nil
}

// NewToken returns a random prefixed secret such as "akj_...".
func NewToken(prefix string) string {
	return prefix + "_" + rand.Text() + rand.Text()
}

// HashToken is the stored form of a secret token.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// EqualHash compares two hashes in constant time.
func EqualHash(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// SHA256Hex hashes arbitrary bytes.
func SHA256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// NewSigningKey generates an Ed25519 key for signing policy bundles.
func NewSigningKey() (ed25519.PrivateKey, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	return priv, err
}

// ValidateCAPEM checks that a PEM bundle holds at least one parseable certificate.
func ValidateCAPEM(data []byte) error {
	n := 0
	for {
		var b *pem.Block
		b, data = pem.Decode(data)
		if b == nil {
			break
		}
		if b.Type != "CERTIFICATE" {
			continue
		}
		if _, err := x509.ParseCertificate(b.Bytes); err != nil {
			return fmt.Errorf("CA certificate is invalid: %w", err)
		}
		n++
	}
	if n == 0 {
		return errors.New("CA certificate must be PEM (-----BEGIN CERTIFICATE-----)")
	}
	return nil
}

// TLSConfigWithCA returns a TLS config trusting the system roots plus the given PEM CA (if any).
func TLSConfigWithCA(caPEM string) (*tls.Config, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if strings.TrimSpace(caPEM) == "" {
		return cfg, nil
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM([]byte(caPEM)) {
		return nil, errors.New("CA certificate contains no usable certificate")
	}
	cfg.RootCAs = pool
	return cfg, nil
}
