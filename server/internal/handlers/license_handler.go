// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package handlers

import (
	"errors"

	"github.com/goakili/akili/server/internal/enterprise"
	"github.com/goakili/akili/server/internal/enterprise/license"
	"github.com/goakili/akili/server/internal/middlewares"
	"github.com/goakili/akili/server/internal/models"
	"github.com/jkaninda/okapi"
)

// LicenseFeature is one Enterprise feature and whether the license grants it.
type LicenseFeature struct {
	enterprise.FlagInfo
	Granted bool `json:"granted"`
}

// LicenseView is the edition, the installed license and what it grants.
type LicenseView struct {
	enterprise.Entitlements
	Features []LicenseFeature `json:"features"`
	// AgentsInUse counts agents that are not revoked, against limits.agents.
	AgentsInUse int64 `json:"agents_in_use"`
}

// LicenseRequest installs a license token.
type LicenseRequest struct {
	Body struct {
		Token string `json:"token" required:"true" maxLength:"16384" description:"An Akili Enterprise license token (akili-v1.…)"`
	} `json:"body"`
}

func (h *Handlers) licenseView(c *okapi.Context) LicenseView {
	ent := h.EE.Entitlements()
	v := LicenseView{Entitlements: ent}
	for _, f := range enterprise.AllFlags {
		v.Features = append(v.Features, LicenseFeature{FlagInfo: f, Granted: ent.Flags[f.Name]})
	}
	h.DB.Model(&models.Agent{}).Where("organization_id = ? AND status <> ?", middlewares.OrgID(c), models.AgentRevoked).Count(&v.AgentsInUse)
	return v
}

// GetLicense returns the edition and the installed license.
func (h *Handlers) GetLicense(c *okapi.Context) error {
	return ok(c, h.licenseView(c))
}

// InstallLicense verifies and installs a license, replacing any other.
func (h *Handlers) InstallLicense(c *okapi.Context, req *LicenseRequest) error {
	ent, err := h.EE.Install(c.Request().Context(), req.Body.Token)
	if errors.Is(err, license.ErrMalformed) || errors.Is(err, license.ErrBadSignature) || errors.Is(err, license.ErrBadKey) {
		return c.AbortBadRequest("this is not a valid Akili Enterprise license: " + err.Error())
	}
	if errors.Is(err, enterprise.ErrBindingMismatch) {
		return c.AbortBadRequest(err.Error())
	}
	if err != nil {
		return mapErr(c, err)
	}
	meta := map[string]any{"license_id": ent.LicenseID, "customer": ent.Customer, "state": ent.State, "install_id": ent.LicenseInstallID}
	if ent.NotAfter != nil {
		meta["not_after"] = ent.NotAfter
	}
	h.record(c, "license.install", "license", ent.LicenseID, meta)
	return ok(c, h.licenseView(c))
}

// RemoveLicense removes the installed license; the control plane continues as Community.
func (h *Handlers) RemoveLicense(c *okapi.Context) error {
	before := h.EE.Entitlements()
	if err := h.EE.Remove(c.Request().Context()); err != nil {
		return mapErr(c, err)
	}
	h.record(c, "license.remove", "license", before.LicenseID, map[string]any{"customer": before.Customer})
	return ok(c, h.licenseView(c))
}
