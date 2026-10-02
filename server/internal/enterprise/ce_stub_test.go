//go:build !enterprise

// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package enterprise

import (
	"context"
	"errors"
	"testing"
)

func TestCommunityStubGrantsNothing(t *testing.T) {
	e := New(nil, "akili-v1.any.token", "https://akili.example.com", "ins_this")
	ent := e.Entitlements()
	if ent.Edition != EditionCommunity || ent.Licensable || ent.Flags == nil || ent.Limits == nil || ent.InstallID != "ins_this" {
		t.Fatalf("entitlements %+v", ent)
	}
	for _, f := range AllFlags {
		if e.Has(f.Name) || e.Mutable(f.Name) || !errors.Is(e.Require(f.Name), ErrLicenseRequired) {
			t.Errorf("%s was granted by the Community stub", f.Name)
		}
	}
	if _, err := e.Install(context.Background(), "akili-v1.any.token"); !errors.Is(err, ErrCommunityEdition) {
		t.Errorf("install: %v", err)
	}
}
