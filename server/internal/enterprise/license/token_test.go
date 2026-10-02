// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package license

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

// testKeys returns a throwaway keypair in the issuer's format (base64 public and private key).
func testKeys(t *testing.T) (pub, priv string) {
	t.Helper()
	p, k, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(p), base64.StdEncoding.EncodeToString(k)
}

func TestSignVerifyRoundTrip(t *testing.T) {
	pub, priv := testKeys(t)
	in := Claims{LicenseID: "lic_1", Customer: "Acme", Edition: "enterprise", Flags: []string{"saml"},
		Limits: map[string]int{"agents": 50}, NotAfter: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), GraceDays: 30}
	tok, err := Sign(priv, in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tok, "akili-v1.") {
		t.Fatalf("token %q lacks the akili-v1 prefix", tok)
	}
	out, err := Verify(pub, tok)
	if err != nil {
		t.Fatal(err)
	}
	if out.Customer != "Acme" || out.Limits["agents"] != 50 || out.Flags[0] != "saml" {
		t.Fatalf("claims changed in transit: %+v", out)
	}
}

func TestVerifyRejectsForgeries(t *testing.T) {
	pub, priv := testKeys(t)
	otherPub, _ := testKeys(t)
	tok, _ := Sign(priv, Claims{Customer: "Acme", Flags: []string{"saml"}})
	parts := strings.Split(tok, ".")

	tampered, _ := Sign(priv, Claims{Customer: "Acme", Flags: []string{"saml", "scim"}})
	swapped := parts[0] + "." + strings.Split(tampered, ".")[1] + "." + parts[2]

	for name, tc := range map[string]struct {
		pub, token string
		want       error
	}{
		"other key":          {otherPub, tok, ErrBadSignature},
		"claims swapped":     {pub, swapped, ErrBadSignature},
		"miabi prefix":       {pub, "miabi-v1." + parts[1] + "." + parts[2], ErrMalformed},
		"two parts":          {pub, parts[0] + "." + parts[1], ErrMalformed},
		"not base64":         {pub, parts[0] + ".!!!." + parts[2], ErrMalformed},
		"invalid public key": {"bm90IGEga2V5", tok, ErrBadKey},
	} {
		if _, err := Verify(tc.pub, tc.token); !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v, want %v", name, err, tc.want)
		}
	}
	if _, err := Sign("bm90IGEga2V5", Claims{}); !errors.Is(err, ErrBadKey) {
		t.Errorf("signing with an invalid key: %v", err)
	}
}

func TestEvaluateStates(t *testing.T) {
	notAfter := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	c := Claims{Flags: []string{"saml"}, NotBefore: notAfter.AddDate(-1, 0, 0), NotAfter: notAfter, GraceDays: 30}
	for name, tc := range map[string]struct {
		now  time.Time
		want State
	}{
		"before start":   {notAfter.AddDate(-2, 0, 0), StateNone},
		"within term":    {notAfter.AddDate(0, -1, 0), StateValid},
		"in grace":       {notAfter.AddDate(0, 0, 10), StateGrace},
		"past grace":     {notAfter.AddDate(0, 0, 31), StateDegraded},
		"at expiry time": {notAfter, StateGrace},
	} {
		s := Evaluate(c, tc.now)
		if s.State != tc.want {
			t.Errorf("%s: state %s, want %s", name, s.State, tc.want)
		}
		if !s.Flags["saml"] {
			t.Errorf("%s: flags lost", name)
		}
	}
}
