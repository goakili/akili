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

// Package migration runs AutoMigrate from the models, then the upgrade package's one-off data steps, on every start.
package migration

import (
	"context"
	"fmt"

	"github.com/goakili/akili/server/internal/models"
	"github.com/goakili/akili/server/internal/storage/migration/upgrade"
	"gorm.io/gorm"
)

func Run(ctx context.Context, db *gorm.DB) error {
	if err := db.WithContext(ctx).AutoMigrate(models.All()...); err != nil {
		return fmt.Errorf("auto-migrate: %w", err)
	}
	return upgrade.Run(ctx, db)
}
