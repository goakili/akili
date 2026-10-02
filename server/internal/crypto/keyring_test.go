// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package crypto

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// v1Encrypt reproduces the pre-envelope format to prove old rows stay readable.
func v1Encrypt(t *testing.T, pass, plain string) string {
	key := sha256.Sum256([]byte("akili-at-rest-v1:" + pass))
	block, _ := aes.NewCipher(key[:])
	aead, _ := cipher.NewGCM(block)
	nonce := make([]byte, aead.NonceSize())
	_, _ = rand.Read(nonce)
	return "v1:" + base64.StdEncoding.EncodeToString(aead.Seal(nonce, nonce, []byte(plain), nil))
}

func localBox(t *testing.T, store KeyStore, pass string, extra ...KEK) *Box {
	t.Helper()
	kek, err := NewLocalKEK(pass)
	if err != nil {
		t.Fatal(err)
	}
	ring, err := OpenKeyring(context.Background(), kek, store, pass, extra...)
	if err != nil {
		t.Fatal(err)
	}
	return NewBox(ring)
}

func TestEnvelopeRoundTripAndLegacy(t *testing.T) {
	const pass = "a-long-enough-test-passphrase-000000"
	store := NewMemoryStore()
	box := localBox(t, store, pass)
	enc, err := box.Encrypt("s3cret")
	if err != nil || !strings.HasPrefix(enc, "v2:"+box.Keyring().Active()+":") {
		t.Fatalf("encrypt: %q %v", enc, err)
	}
	if got, err := box.Decrypt(enc); err != nil || got != "s3cret" {
		t.Fatalf("decrypt: %q %v", got, err)
	}
	old := v1Encrypt(t, pass, "legacy")
	if got, err := box.Decrypt(old); err != nil || got != "legacy" {
		t.Fatalf("legacy decrypt: %q %v", got, err)
	}
	re, changed, err := box.Reencrypt(old)
	if err != nil || !changed || KeyID(re) != box.Keyring().Active() {
		t.Fatalf("reencrypt: %q %v %v", re, changed, err)
	}
	wrong, _ := NewLocalKEK("a-different-passphrase-00000000000")
	if _, err := OpenKeyring(context.Background(), wrong, store, ""); err == nil {
		t.Fatal("wrong passphrase opened the keyring")
	}
	// Tampering with the key id must not decrypt under another key.
	parts := strings.SplitN(enc, ":", 3)
	if _, err := box.Decrypt("v2:" + parts[1] + ":" + base64.StdEncoding.EncodeToString([]byte("garbage-garbage-garbage"))); err == nil {
		t.Fatal("garbage decrypted")
	}
}

func TestRotateSeenByOtherReplica(t *testing.T) {
	const pass = "a-long-enough-test-passphrase-000000"
	store := NewMemoryStore()
	a := localBox(t, store, pass)
	b := localBox(t, store, pass)
	if a.Keyring().Active() != b.Keyring().Active() {
		t.Fatal("replicas started with different active keys")
	}
	before, _ := a.Encrypt("one")
	kid, err := a.Keyring().Rotate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	after, _ := a.Encrypt("two")
	if KeyID(after) != kid || KeyID(before) == kid {
		t.Fatalf("rotation did not switch keys: %s %s %s", KeyID(before), KeyID(after), kid)
	}
	for _, c := range []string{before, after} {
		if _, err := b.Decrypt(c); err != nil {
			t.Fatalf("replica b cannot read %s: %v", KeyID(c), err)
		}
	}
}

// fakeTransit is a minimal Vault Transit API.
func fakeTransit(t *testing.T, token string) *httptest.Server {
	var mu sync.Mutex
	vault := map[string]string{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != token {
			w.WriteHeader(403)
			_, _ = w.Write([]byte(`{"errors":["permission denied"]}`))
			return
		}
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/v1/transit/encrypt/akili":
			ct := "vault:v1:" + base64.StdEncoding.EncodeToString([]byte(rand.Text()))
			vault[ct] = in["plaintext"]
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"ciphertext": ct}})
		case "/v1/transit/decrypt/akili":
			pt, ok := vault[in["ciphertext"]]
			if !ok {
				w.WriteHeader(400)
				_, _ = w.Write([]byte(`{"errors":["invalid ciphertext"]}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"plaintext": pt}})
		default:
			w.WriteHeader(404)
		}
	}))
}

func TestRewrapToVaultTransit(t *testing.T) {
	const pass = "a-long-enough-test-passphrase-000000"
	srv := fakeTransit(t, "root")
	defer srv.Close()
	store := NewMemoryStore()
	local := localBox(t, store, pass)
	enc, _ := local.Encrypt("provider-key")

	vault := &VaultTransitKEK{Addr: srv.URL, Token: "root", Key: "akili"}
	localKEK, _ := NewLocalKEK(pass)
	ring, err := OpenKeyring(context.Background(), vault, store, pass, localKEK)
	if err != nil {
		t.Fatal(err)
	}
	n, err := ring.Rewrap(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("rewrap: %d %v", n, err)
	}
	// Without the local passphrase at all, the vault-wrapped key still opens.
	ring2, err := OpenKeyring(context.Background(), vault, store, "")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := NewBox(ring2).Decrypt(enc); err != nil || got != "provider-key" {
		t.Fatalf("after rewrap: %q %v", got, err)
	}
	keys, _ := ring2.Keys(context.Background())
	if len(keys) != 1 || keys[0].Provider != "vault-transit" || !keys[0].Active {
		t.Fatalf("keys: %+v", keys)
	}
	if _, err := OpenKeyring(context.Background(), &VaultTransitKEK{Addr: srv.URL, Token: "wrong", Key: "akili"}, store, ""); err == nil {
		t.Fatal("a wrong vault token opened the keyring")
	}
}
