// Copyright (c) 2026 Jonas Kaninda.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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

func (s SettingsKeyStore) Get(ctx context.Context, key string) (string, bool, error) {
	var row models.Setting
	err := s.DB.WithContext(ctx).First(&row, "key = ?", key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	return row.Value, err == nil, err
}

func (s SettingsKeyStore) Create(ctx context.Context, key, value string) (bool, error) {
	res := s.DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&models.Setting{Key: key, Value: value, UpdatedAt: time.Now().UTC()})
	return res.RowsAffected == 1, res.Error
}

func (s SettingsKeyStore) Put(ctx context.Context, key, value string) error {
	return s.DB.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"})}).
		Create(&models.Setting{Key: key, Value: value, UpdatedAt: time.Now().UTC()}).Error
}

func (s SettingsKeyStore) Keys(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	err := s.DB.WithContext(ctx).Model(&models.Setting{}).Where("key LIKE ?", prefix+"%").Order("key").Pluck("key", &keys).Error
	return keys, err
}
