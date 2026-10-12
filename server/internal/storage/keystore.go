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
	"time"

	"github.com/goakili/akili/server/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SettingsKeyStore keeps wrapped data keys in the settings table (crypto.KeyStore).
type SettingsKeyStore struct{ DB *gorm.DB }

// Get implements crypto.KeyStore.
func (s SettingsKeyStore) Get(ctx context.Context, key string) (string, bool, error) {
	var row models.Setting
	err := s.DB.WithContext(ctx).First(&row, "key = ?", key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	return row.Value, err == nil, err
}

// Create implements crypto.KeyStore.
func (s SettingsKeyStore) Create(ctx context.Context, key, value string) (bool, error) {
	res := s.DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&models.Setting{Key: key, Value: value, UpdatedAt: time.Now().UTC()})
	return res.RowsAffected == 1, res.Error
}

// Put implements crypto.KeyStore.
func (s SettingsKeyStore) Put(ctx context.Context, key, value string) error {
	return s.DB.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"})}).
		Create(&models.Setting{Key: key, Value: value, UpdatedAt: time.Now().UTC()}).Error
}

// Keys implements crypto.KeyStore.
func (s SettingsKeyStore) Keys(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	err := s.DB.WithContext(ctx).Model(&models.Setting{}).Where("key LIKE ?", prefix+"%").Order("key").Pluck("key", &keys).Error
	return keys, err
}
