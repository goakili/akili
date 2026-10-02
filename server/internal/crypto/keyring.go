// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package crypto

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Envelope encryption: each secret is sealed with a data key (DEK); data keys are stored wrapped by a
// key-encryption key (KEK) that is either derived locally from AKILI_ENCRYPTION_KEY or held in a KMS
// (Vault Transit), so with a KMS the database alone never yields a secret.

// KEK wraps and unwraps data keys.
type KEK interface {
	// Name prefixes wrapped keys so a key can be unwrapped by the provider that wrapped it.
	Name() string
	Wrap(ctx context.Context, dek []byte) (string, error)
	Unwrap(ctx context.Context, wrapped string) ([]byte, error)
}

// KeyStore persists wrapped data keys (the settings table).
type KeyStore interface {
	Get(ctx context.Context, key string) (value string, found bool, err error)
	// Create inserts the key unless it exists and reports whether it did.
	Create(ctx context.Context, key, value string) (bool, error)
	Put(ctx context.Context, key, value string) error
	Keys(ctx context.Context, prefix string) ([]string, error)
}

const (
	dekPrefix    = "crypto.dek."
	activeDEKKey = "crypto.active_dek"
	v2Prefix     = "v2:"
)

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func seal(aead cipher.AEAD, plain, aad []byte) ([]byte, error) {
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, plain, aad), nil
}

func unseal(aead cipher.AEAD, raw, aad []byte) ([]byte, error) {
	ns := aead.NonceSize()
	if len(raw) < ns {
		return nil, errors.New("ciphertext too short")
	}
	return aead.Open(nil, raw[:ns], raw[ns:], aad)
}

// LocalKEK derives the KEK from a passphrase (AKILI_ENCRYPTION_KEY).
type LocalKEK struct{ aead cipher.AEAD }

// NewLocalKEK returns a KEK derived from passphrase.
func NewLocalKEK(passphrase string) (*LocalKEK, error) {
	if passphrase == "" {
		return nil, errors.New("encryption key is empty")
	}
	key := sha256.Sum256([]byte("akili-kek-v1:" + passphrase))
	aead, err := newAEAD(key[:])
	if err != nil {
		return nil, err
	}
	return &LocalKEK{aead: aead}, nil
}

// Name implements KEK.
func (k *LocalKEK) Name() string { return "local" }

// Wrap implements KEK.
func (k *LocalKEK) Wrap(_ context.Context, dek []byte) (string, error) {
	sealed, err := seal(k.aead, dek, []byte("akili-dek"))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Unwrap implements KEK.
func (k *LocalKEK) Unwrap(_ context.Context, wrapped string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(wrapped)
	if err != nil {
		return nil, err
	}
	dek, err := unseal(k.aead, raw, []byte("akili-dek"))
	if err != nil {
		return nil, errors.New("cannot unwrap data key: wrong AKILI_ENCRYPTION_KEY")
	}
	return dek, nil
}

// VaultTransitKEK wraps data keys with a HashiCorp Vault (or OpenBao) Transit key. The KEK never
// leaves Vault; the control plane only holds a token allowed to encrypt and decrypt with that key.
type VaultTransitKEK struct {
	Addr      string // e.g. https://vault.example.com:8200
	Token     string
	Mount     string // default "transit"
	Key       string
	Namespace string
	Client    *http.Client
}

// Name implements KEK.
func (v *VaultTransitKEK) Name() string { return "vault-transit" }

func (v *VaultTransitKEK) call(ctx context.Context, op string, in map[string]string, out any) error {
	mount := v.Mount
	if mount == "" {
		mount = "transit"
	}
	body, _ := json.Marshal(in)
	url := fmt.Sprintf("%s/v1/%s/%s/%s", strings.TrimRight(v.Addr, "/"), strings.Trim(mount, "/"), op, v.Key)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("X-Vault-Token", v.Token)
	req.Header.Set("Content-Type", "application/json")
	if v.Namespace != "" {
		req.Header.Set("X-Vault-Namespace", v.Namespace)
	}
	c := v.Client
	if c == nil {
		c = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("vault: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		var e struct {
			Errors []string `json:"errors"`
		}
		_ = json.Unmarshal(raw, &e)
		return fmt.Errorf("vault transit %s: %d %s", op, resp.StatusCode, strings.Join(e.Errors, "; "))
	}
	return json.Unmarshal(raw, out)
}

// Wrap implements KEK.
func (v *VaultTransitKEK) Wrap(ctx context.Context, dek []byte) (string, error) {
	var out struct {
		Data struct {
			Ciphertext string `json:"ciphertext"`
		} `json:"data"`
	}
	if err := v.call(ctx, "encrypt", map[string]string{"plaintext": base64.StdEncoding.EncodeToString(dek)}, &out); err != nil {
		return "", err
	}
	if out.Data.Ciphertext == "" {
		return "", errors.New("vault transit: empty ciphertext")
	}
	return out.Data.Ciphertext, nil
}

// Unwrap implements KEK.
func (v *VaultTransitKEK) Unwrap(ctx context.Context, wrapped string) ([]byte, error) {
	var out struct {
		Data struct {
			Plaintext string `json:"plaintext"`
		} `json:"data"`
	}
	if err := v.call(ctx, "decrypt", map[string]string{"ciphertext": wrapped}, &out); err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(out.Data.Plaintext)
}

// Keyring holds the unwrapped data keys.
type Keyring struct {
	kek     KEK
	others  map[string]KEK // other providers that can unwrap older keys (rewrap)
	store   KeyStore
	mu      sync.RWMutex
	keys    map[string]cipher.AEAD
	active  string
	legacy  cipher.AEAD // v1 ciphertexts: key derived directly from AKILI_ENCRYPTION_KEY
	loadCtx func() (context.Context, context.CancelFunc)
}

// OpenKeyring loads (or, on first start, creates) the active data key. legacyPassphrase decrypts
// v1 ciphertexts written before envelope encryption; it may be empty when none remain. Extra KEKs
// let keys wrapped by a previous provider be read (after switching to a KMS).
func OpenKeyring(ctx context.Context, kek KEK, store KeyStore, legacyPassphrase string, extra ...KEK) (*Keyring, error) {
	k := &Keyring{kek: kek, store: store, keys: map[string]cipher.AEAD{}, others: map[string]KEK{},
		loadCtx: func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), 15*time.Second)
		}}
	for _, e := range extra {
		if e != nil && e.Name() != kek.Name() {
			k.others[e.Name()] = e
		}
	}
	if legacyPassphrase != "" {
		key := sha256.Sum256([]byte("akili-at-rest-v1:" + legacyPassphrase))
		aead, err := newAEAD(key[:])
		if err != nil {
			return nil, err
		}
		k.legacy = aead
	}
	active, found, err := store.Get(ctx, activeDEKKey)
	if err != nil {
		return nil, err
	}
	if !found {
		kid, err := k.newDataKey(ctx)
		if err != nil {
			return nil, err
		}
		// Two replicas starting at once: only one active key wins; the other's key stays usable.
		if _, err := store.Create(ctx, activeDEKKey, kid); err != nil {
			return nil, err
		}
		if active, _, err = store.Get(ctx, activeDEKKey); err != nil {
			return nil, err
		}
	}
	if _, err := k.load(ctx, active); err != nil {
		return nil, fmt.Errorf("active data key %s: %w", active, err)
	}
	k.active = active
	return k, nil
}

func newKID() string { return strings.ToLower(rand.Text()[:12]) }

// newDataKey generates and stores a wrapped data key, returning its id.
func (k *Keyring) newDataKey(ctx context.Context) (string, error) {
	dek := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return "", err
	}
	wrapped, err := k.kek.Wrap(ctx, dek)
	if err != nil {
		return "", err
	}
	kid := newKID()
	if _, err := k.store.Create(ctx, dekPrefix+kid, k.kek.Name()+":"+wrapped); err != nil {
		return "", err
	}
	aead, err := newAEAD(dek)
	if err != nil {
		return "", err
	}
	k.mu.Lock()
	k.keys[kid] = aead
	k.mu.Unlock()
	return kid, nil
}

func (k *Keyring) provider(name string) (KEK, error) {
	if name == k.kek.Name() {
		return k.kek, nil
	}
	if p, ok := k.others[name]; ok {
		return p, nil
	}
	return nil, fmt.Errorf("data key is wrapped by %q, which is not configured", name)
}

// load unwraps a data key (cached after the first use).
func (k *Keyring) load(ctx context.Context, kid string) (cipher.AEAD, error) {
	k.mu.RLock()
	aead, ok := k.keys[kid]
	k.mu.RUnlock()
	if ok {
		return aead, nil
	}
	v, found, err := k.store.Get(ctx, dekPrefix+kid)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("unknown data key %s", kid)
	}
	name, wrapped, ok := strings.Cut(v, ":")
	if !ok {
		return nil, errors.New("malformed data key")
	}
	p, err := k.provider(name)
	if err != nil {
		return nil, err
	}
	dek, err := p.Unwrap(ctx, wrapped)
	if err != nil {
		return nil, err
	}
	if aead, err = newAEAD(dek); err != nil {
		return nil, err
	}
	k.mu.Lock()
	k.keys[kid] = aead
	k.mu.Unlock()
	return aead, nil
}

// Active returns the id of the data key new secrets are sealed with.
func (k *Keyring) Active() string {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.active
}

// Provider names the KEK provider in use.
func (k *Keyring) Provider() string { return k.kek.Name() }

func (k *Keyring) encrypt(plain string) (string, error) {
	kid := k.Active()
	aead, err := k.load(context.Background(), kid)
	if err != nil {
		return "", err
	}
	sealed, err := seal(aead, []byte(plain), []byte(kid))
	if err != nil {
		return "", err
	}
	return v2Prefix + kid + ":" + base64.StdEncoding.EncodeToString(sealed), nil
}

func (k *Keyring) decrypt(enc string) (string, error) {
	switch {
	case strings.HasPrefix(enc, v2Prefix):
		kid, b64, ok := strings.Cut(strings.TrimPrefix(enc, v2Prefix), ":")
		if !ok {
			return "", errors.New("malformed ciphertext")
		}
		raw, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return "", err
		}
		// A key rotated in by another replica is loaded on first use.
		ctx, cancel := k.loadCtx()
		defer cancel()
		aead, err := k.load(ctx, kid)
		if err != nil {
			return "", err
		}
		plain, err := unseal(aead, raw, []byte(kid))
		if err != nil {
			return "", fmt.Errorf("decrypt: %w", err)
		}
		return string(plain), nil
	case strings.HasPrefix(enc, boxPrefix):
		if k.legacy == nil {
			return "", errors.New("v1 ciphertext needs AKILI_ENCRYPTION_KEY (run `akili keys rotate` to re-encrypt it)")
		}
		raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(enc, boxPrefix))
		if err != nil {
			return "", err
		}
		plain, err := unseal(k.legacy, raw, nil)
		if err != nil {
			return "", fmt.Errorf("decrypt: %w", err)
		}
		return string(plain), nil
	}
	return "", errors.New("unknown ciphertext format")
}

// Rotate creates a new data key and makes it active. Existing secrets stay readable; re-encrypt
// them (Box.Reencrypt) to retire the old key.
func (k *Keyring) Rotate(ctx context.Context) (string, error) {
	kid, err := k.newDataKey(ctx)
	if err != nil {
		return "", err
	}
	if err := k.store.Put(ctx, activeDEKKey, kid); err != nil {
		return "", err
	}
	k.mu.Lock()
	k.active = kid
	k.mu.Unlock()
	return kid, nil
}

// Rewrap re-wraps every stored data key with the current KEK (e.g. after moving to a KMS or after
// the KMS key was rotated). It returns how many keys were rewrapped.
func (k *Keyring) Rewrap(ctx context.Context) (int, error) {
	ids, err := k.store.Keys(ctx, dekPrefix)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, full := range ids {
		kid := strings.TrimPrefix(full, dekPrefix)
		if _, err := k.load(ctx, kid); err != nil {
			return n, fmt.Errorf("data key %s: %w", kid, err)
		}
		v, _, err := k.store.Get(ctx, full)
		if err != nil {
			return n, err
		}
		name, wrapped, _ := strings.Cut(v, ":")
		p, err := k.provider(name)
		if err != nil {
			return n, err
		}
		dek, err := p.Unwrap(ctx, wrapped)
		if err != nil {
			return n, err
		}
		rewrapped, err := k.kek.Wrap(ctx, dek)
		if err != nil {
			return n, err
		}
		if err := k.store.Put(ctx, full, k.kek.Name()+":"+rewrapped); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// KeyInfo describes one stored data key.
type KeyInfo struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Active   bool   `json:"active"`
}

// Keys lists the stored data keys.
func (k *Keyring) Keys(ctx context.Context) ([]KeyInfo, error) {
	ids, err := k.store.Keys(ctx, dekPrefix)
	if err != nil {
		return nil, err
	}
	out := make([]KeyInfo, 0, len(ids))
	for _, full := range ids {
		v, _, err := k.store.Get(ctx, full)
		if err != nil {
			return nil, err
		}
		name, _, _ := strings.Cut(v, ":")
		kid := strings.TrimPrefix(full, dekPrefix)
		out = append(out, KeyInfo{ID: kid, Provider: name, Active: kid == k.Active()})
	}
	return out, nil
}

// KeyID reports which key sealed a ciphertext: a data key id, "legacy" for v1, or "" when empty.
func KeyID(enc string) string {
	switch {
	case enc == "":
		return ""
	case strings.HasPrefix(enc, v2Prefix):
		kid, _, _ := strings.Cut(strings.TrimPrefix(enc, v2Prefix), ":")
		return kid
	case strings.HasPrefix(enc, boxPrefix):
		return "legacy"
	}
	return "unknown"
}
