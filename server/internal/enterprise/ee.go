//go:build enterprise

// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: LicenseRef-Akili-Enterprise

package enterprise

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/goakili/akili/server/internal/enterprise/license"
	"github.com/goakili/akili/server/internal/models"
	"github.com/jkaninda/logger"
	"gorm.io/gorm"
)

// embeddedPublicKey is set at build time (-ldflags -X); there is no runtime override, so an
// operator cannot swap in their own key and sign their own licenses.
var embeddedPublicKey string

// New loads the installed license from the database, or installs token (AKILI_LICENSE) when none
// is stored. installID and publicURL identify this deployment, for licenses bound to it.
func New(db *gorm.DB, token, publicURL, installID string) EE {
	return newEE(db, strings.TrimSpace(embeddedPublicKey), token, publicURL, installID)
}

func newEE(db *gorm.DB, pub, token, publicURL, installID string) *impl {
	e := &impl{db: db, pub: pub, host: hostOf(publicURL), installID: strings.TrimSpace(installID), now: time.Now}
	if pub == "" {
		logger.Warn("enterprise: no license public key in this build; licenses cannot be verified")
		return e
	}
	if err := e.load(); err != nil {
		logger.Warn("enterprise: could not load the installed license", "error", err)
	}
	if e.claims == nil && strings.TrimSpace(token) != "" {
		if _, err := e.Install(context.Background(), token); err != nil {
			logger.Warn("enterprise: AKILI_LICENSE was not installed", "error", err)
		}
	}
	if c := e.current(); c != nil {
		logger.Info("enterprise: license active", "customer", c.Customer, "license", c.LicenseID, "expires", c.NotAfter)
	}
	return e
}

type impl struct {
	db        *gorm.DB
	pub       string
	host      string
	installID string
	now       func() time.Time

	mu       sync.RWMutex
	claims   *license.Claims // nil: community
	mismatch string          // why an authentic license does not match this deployment; "" if it does
}

func (e *impl) current() *license.Claims {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.claims
}

// load re-verifies the stored token: the signed token is authoritative, so an edited row grants
// nothing.
func (e *impl) load() error {
	var row models.License
	if err := e.db.Order("id DESC").First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	c, err := license.Verify(e.pub, row.Token)
	if err != nil {
		logger.Warn("enterprise: the stored license failed verification; running as Community", "error", err)
		return nil
	}
	e.set(&c)
	return nil
}

func (e *impl) set(c *license.Claims) {
	mismatch := ""
	if c != nil {
		mismatch = e.bindingMismatch(c)
	}
	e.mu.Lock()
	e.claims, e.mismatch = c, mismatch
	e.mu.Unlock()
	if mismatch != "" {
		logger.Warn("enterprise: the license is bound to another deployment; no features granted", "reason", mismatch)
	}
}

// bindingMismatch explains why a license does not match this deployment, or returns "". Bindings are
// conjunctive: the Install ID (exact) and the public URL host, each only when the license sets it.
func (e *impl) bindingMismatch(c *license.Claims) string {
	if want := strings.TrimSpace(c.InstallID); want != "" && !strings.EqualFold(want, e.installID) {
		return "the license is for install ID " + want + ", this deployment is " + e.installID
	}
	if want := hostOf(c.URL); want != "" && want != e.host {
		return "the license is for " + want + ", this deployment is " + e.host
	}
	return ""
}

func hostOf(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	if u, err := url.Parse(s); err == nil {
		return u.Hostname()
	}
	return ""
}

func (e *impl) snapshot() (license.Snapshot, string) {
	e.mu.RLock()
	c, mismatch := e.claims, e.mismatch
	e.mu.RUnlock()
	if c == nil {
		return license.Snapshot{State: license.StateNone}, ""
	}
	return license.Evaluate(*c, e.now()), mismatch
}

func (e *impl) Entitlements() Entitlements {
	s, mismatch := e.snapshot()
	ent := Entitlements{Edition: s.Edition, State: string(s.State), Customer: s.Customer, LicenseID: s.LicenseID,
		InstallID: e.installID, LicenseInstallID: s.InstallID, URL: s.URL, Licensable: e.pub != "", Flags: s.Flags, Limits: s.Limits}
	if s.State != license.StateNone {
		na, ge := s.NotAfter, s.GraceEnds
		ent.NotAfter, ent.GraceEnds = &na, &ge
	}
	if mismatch != "" {
		ent.State, ent.BindingError = StateBindingMismatch, mismatch
		ent.Flags, ent.Limits = nil, nil
	}
	if ent.State == string(license.StateNone) {
		ent.Edition = EditionCommunity
	}
	return emptyEntitlements(ent)
}

// Has stays true once the license is past its grace period: an Enterprise feature already in use
// keeps working; only its configuration freezes (see Mutable).
func (e *impl) Has(flag string) bool {
	s, mismatch := e.snapshot()
	return mismatch == "" && s.State != license.StateNone && s.Flags[flag]
}

func (e *impl) Mutable(flag string) bool {
	s, mismatch := e.snapshot()
	return mismatch == "" && (s.State == license.StateValid || s.State == license.StateGrace) && s.Flags[flag]
}

func (e *impl) Require(flag string) error {
	s, mismatch := e.snapshot()
	switch {
	case mismatch != "":
		return ErrBindingMismatch
	case s.State == license.StateNone:
		return ErrLicenseRequired
	case !s.Flags[flag]:
		return ErrEntitlementDenied
	}
	return nil
}

func (e *impl) RequireMutable(flag string) error {
	if err := e.Require(flag); err != nil {
		return err
	}
	if !e.Mutable(flag) {
		return ErrLicenseExpired
	}
	return nil
}

// Install verifies the token and stores it as the only license, replacing any other.
func (e *impl) Install(ctx context.Context, token string) (Entitlements, error) {
	if e.pub == "" {
		return Entitlements{}, ErrNoPublicKey
	}
	token = strings.TrimSpace(token)
	c, err := license.Verify(e.pub, token)
	if err != nil {
		return Entitlements{}, err
	}
	if reason := e.bindingMismatch(&c); reason != "" {
		return Entitlements{}, fmt.Errorf("%w: %s", ErrBindingMismatch, reason)
	}
	row := models.License{LicenseID: c.LicenseID, Customer: c.Customer, Token: token, NotAfter: c.NotAfter}
	err = e.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("1 = 1").Delete(&models.License{}).Error; err != nil {
			return err
		}
		return tx.Create(&row).Error
	})
	if err != nil {
		return Entitlements{}, err
	}
	e.set(&c)
	return e.Entitlements(), nil
}

func (e *impl) Remove(ctx context.Context) error {
	if err := e.db.WithContext(ctx).Where("1 = 1").Delete(&models.License{}).Error; err != nil {
		return err
	}
	e.set(nil)
	return nil
}
