//go:build !enterprise

// Copyright (c) 2026 Jonas Kaninda.
//
// This file is part of Akili.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.
//
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
