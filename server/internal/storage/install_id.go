// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package storage

import (
	"context"
	"errors"

	"github.com/goakili/akili/server/internal/models"
	"gorm.io/gorm"
)

// InstallIDSetting is the settings key of this deployment's Install ID.
const InstallIDSetting = "install_id"

// EnsureInstallID returns this deployment's Install ID, creating it on first start. It never
// changes afterwards: a customer quotes it when buying a license, and the license is bound to it.
// Replicas starting together agree on one value: the first insert wins and everyone reads it back.
func EnsureInstallID(ctx context.Context, db *gorm.DB) (string, error) {
	store := SettingsKeyStore{DB: db}
	if _, err := store.Create(ctx, InstallIDSetting, models.NewID("ins")); err != nil {
		return "", err
	}
	id, ok, err := store.Get(ctx, InstallIDSetting)
	if err == nil && (!ok || id == "") {
		err = errors.New("install id missing after creation")
	}
	return id, err
}
