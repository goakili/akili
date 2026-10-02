//go:build !enterprise

// SPDX-FileCopyrightText: 2026 Jonas Kaninda
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
