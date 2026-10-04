// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"reflect"
	"testing"
	"time"
)

// goldenToken is signed by the issuer's tool (the private akili-keygen), which keeps its own copy of
// this format; its TestGoldenToken produces the same string. Change both together.
const goldenToken = "akili-v1.eyJsaWNlbnNlX2lkIjoibGljX2dvbGRlbiIsImN1c3RvbWVyIjoiR29sZGVuIENvIiwiZWRpdGlvbiI6ImVudGVycHJpc2UiLCJpbnN0YWxsX2lkIjoiaW5zX2dvbGRlbiIsInVybCI6Imh0dHBzOi8vYWtpbGkuZXhhbXBsZS5jb20iLCJmbGFncyI6WyJzYW1sIiwic2NpbSJdLCJsaW1pdHMiOnsiYWdlbnRzIjoyNX0sIm5vdF9iZWZvcmUiOiIyMDI2LTEwLTAxVDAwOjAwOjAwWiIsIm5vdF9hZnRlciI6IjIwMjctMTAtMDFUMDA6MDA6MDBaIiwiZ3JhY2VfZGF5cyI6MzAsImlzc3VlZF9hdCI6IjIwMjYtMTAtMDFUMDA6MDA6MDBaIn0.f0celVZBo0IpNKBYj3vEPZbHALZcPEzjk_HV8OAsn7G34uaFe4Nu_qhbD98kIzFYKnM13AUb_dkPhA8qH1cCAA"

func goldenKeys() (pub, priv string) {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	k := ed25519.NewKeyFromSeed(seed)
	return base64.StdEncoding.EncodeToString(k.Public().(ed25519.PublicKey)), base64.StdEncoding.EncodeToString(k)
}

func goldenClaims() Claims {
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	return Claims{LicenseID: "lic_golden", Customer: "Golden Co", Edition: "enterprise", InstallID: "ins_golden",
		URL: "https://akili.example.com", Flags: []string{"saml", "scim"}, Limits: map[string]int{"agents": 25},
		NotBefore: day, NotAfter: day.AddDate(1, 0, 0), GraceDays: 30, IssuedAt: day}
}

func TestGoldenToken(t *testing.T) {
	pub, priv := goldenKeys()
	tok, err := Sign(priv, goldenClaims())
	if err != nil || tok != goldenToken {
		t.Fatalf("the token format changed; update akili-keygen with it:\n got %s\nwant %s (%v)", tok, goldenToken, err)
	}
	c, err := Verify(pub, goldenToken)
	if err != nil || !reflect.DeepEqual(c, goldenClaims()) {
		t.Fatalf("verify golden: %+v %v", c, err)
	}
}
