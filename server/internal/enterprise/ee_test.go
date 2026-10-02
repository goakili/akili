//go:build enterprise

// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: LicenseRef-Akili-Enterprise

package enterprise

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/goakili/akili/server/internal/enterprise/license"
)

var expiry = time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

func testKeys(t *testing.T) (pub, priv string) {
	t.Helper()
	p, k, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(p), base64.StdEncoding.EncodeToString(k)
}

func licensed(t *testing.T, now time.Time, c license.Claims) *impl {
	t.Helper()
	pub, _ := testKeys(t)
	e := &impl{pub: pub, host: "akili.example.com", installID: "ins_this", now: func() time.Time { return now }}
	e.set(&c)
	return e
}

func TestCommunityWhenNoLicense(t *testing.T) {
	pub, _ := testKeys(t)
	e := &impl{pub: pub, host: "akili.example.com", installID: "ins_this", now: time.Now}
	ent := e.Entitlements()
	if ent.Edition != EditionCommunity || ent.State != "none" || !ent.Licensable || len(ent.Flags) != 0 || ent.InstallID != "ins_this" {
		t.Fatalf("entitlements without a license: %+v", ent)
	}
	if e.Has(FlagSAML) || !errors.Is(e.Require(FlagSAML), ErrLicenseRequired) {
		t.Fatal("a feature was granted without a license")
	}
}

func TestLapsedLicenseKeepsFeaturesRunningButFreezesThem(t *testing.T) {
	c := license.Claims{Edition: EditionEnterprise, Customer: "Acme", Flags: []string{FlagSAML}, NotAfter: expiry, GraceDays: 30}
	for name, tc := range map[string]struct {
		now          time.Time
		has, mutable bool
		mutableErr   error
	}{
		"valid":    {expiry.AddDate(0, -1, 0), true, true, nil},
		"grace":    {expiry.AddDate(0, 0, 5), true, true, nil},
		"degraded": {expiry.AddDate(0, 0, 40), true, false, ErrLicenseExpired},
	} {
		e := licensed(t, tc.now, c)
		if e.Has(FlagSAML) != tc.has || e.Mutable(FlagSAML) != tc.mutable {
			t.Errorf("%s: has=%v mutable=%v", name, e.Has(FlagSAML), e.Mutable(FlagSAML))
		}
		if err := e.RequireMutable(FlagSAML); !errors.Is(err, tc.mutableErr) {
			t.Errorf("%s: RequireMutable = %v, want %v", name, err, tc.mutableErr)
		}
		if err := e.Require(FlagSCIM); !errors.Is(err, ErrEntitlementDenied) {
			t.Errorf("%s: an unlicensed flag returned %v", name, err)
		}
		if ent := e.Entitlements(); ent.Edition != EditionEnterprise || ent.NotAfter == nil || ent.GraceEnds == nil {
			t.Errorf("%s: entitlements %+v", name, ent)
		}
	}
}

func TestLicenseBoundToAnotherDeploymentGrantsNothing(t *testing.T) {
	e := licensed(t, expiry.AddDate(0, -1, 0), license.Claims{Edition: EditionEnterprise, URL: "https://akili.other.org",
		Flags: []string{FlagSAML}, NotAfter: expiry})
	if e.Has(FlagSAML) || !errors.Is(e.Require(FlagSAML), ErrBindingMismatch) {
		t.Fatal("a license for another deployment granted a feature")
	}
	ent := e.Entitlements()
	if ent.State != StateBindingMismatch || ent.BindingError == "" || len(ent.Flags) != 0 {
		t.Fatalf("entitlements %+v", ent)
	}
	ok := licensed(t, expiry.AddDate(0, -1, 0), license.Claims{Edition: EditionEnterprise, URL: "AKILI.example.com:443",
		Flags: []string{FlagSAML}, NotAfter: expiry})
	if !ok.Has(FlagSAML) {
		t.Fatal("a license bound to this host (any case, any port) was refused")
	}
}

func TestInstallIDBinding(t *testing.T) {
	now := expiry.AddDate(0, -1, 0)
	for name, tc := range map[string]struct {
		installID, url string
		granted        bool
	}{
		"this install":            {"ins_this", "", true},
		"this install, any case":  {"INS_THIS", "", true},
		"another install":         {"ins_other", "", false},
		"this install, other URL": {"ins_this", "akili.other.org", false},
		"this install and URL":    {"ins_this", "https://akili.example.com", true},
		"no binding":              {"", "", true},
	} {
		e := licensed(t, now, license.Claims{Edition: EditionEnterprise, InstallID: tc.installID, URL: tc.url,
			Flags: []string{FlagSAML}, NotAfter: expiry})
		if e.Has(FlagSAML) != tc.granted {
			t.Errorf("%s: granted=%v, want %v (%s)", name, e.Has(FlagSAML), tc.granted, e.Entitlements().BindingError)
		}
		if ent := e.Entitlements(); ent.InstallID != "ins_this" || ent.LicenseInstallID != tc.installID {
			t.Errorf("%s: install ids %q / %q", name, ent.InstallID, ent.LicenseInstallID)
		}
	}
}

func TestInstallRefusesBadTokensBeforeTouchingTheDatabase(t *testing.T) {
	pub, priv := testKeys(t)
	_, otherPriv := testKeys(t)
	e := &impl{pub: pub, host: "akili.example.com", installID: "ins_this", now: time.Now}
	forged, _ := license.Sign(otherPriv, license.Claims{Flags: []string{FlagSAML}, NotAfter: expiry})
	if _, err := e.Install(context.Background(), forged); !errors.Is(err, license.ErrBadSignature) {
		t.Fatalf("forged token: %v", err)
	}
	other, _ := license.Sign(priv, license.Claims{URL: "akili.other.org", NotAfter: expiry})
	if _, err := e.Install(context.Background(), other); !errors.Is(err, ErrBindingMismatch) {
		t.Fatalf("token for another deployment: %v", err)
	}
	otherInstall, _ := license.Sign(priv, license.Claims{InstallID: "ins_other", NotAfter: expiry})
	if _, err := e.Install(context.Background(), otherInstall); !errors.Is(err, ErrBindingMismatch) {
		t.Fatalf("token for another install: %v", err)
	}
	keyless := &impl{now: time.Now}
	if _, err := keyless.Install(context.Background(), forged); !errors.Is(err, ErrNoPublicKey) {
		t.Fatalf("build without a public key: %v", err)
	}
}
