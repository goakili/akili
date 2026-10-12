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
