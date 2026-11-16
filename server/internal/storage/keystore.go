// Copyright (c) 2026 Jonas Kaninda.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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
