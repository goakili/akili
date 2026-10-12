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

	"gorm.io/gorm"
)

// New returns the Community stub: it grants no licensed feature and links no verification code.
func New(_ *gorm.DB, _, _, installID string) EE { return ceStub{installID: installID} }

type ceStub struct{ installID string }

func (s ceStub) Entitlements() Entitlements {
	return emptyEntitlements(Entitlements{State: "none", InstallID: s.installID})
}
func (ceStub) Has(string) bool             { return false }
func (ceStub) Mutable(string) bool         { return false }
func (ceStub) Require(string) error        { return ErrLicenseRequired }
func (ceStub) RequireMutable(string) error { return ErrLicenseRequired }
func (ceStub) Install(context.Context, string) (Entitlements, error) {
	return Entitlements{}, ErrCommunityEdition
}
func (ceStub) Remove(context.Context) error { return ErrCommunityEdition }
