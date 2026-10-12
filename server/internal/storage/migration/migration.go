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

// Package migration brings the database up to date on every start: AutoMigrate creates and extends
// the tables from the models, then the upgrade package applies one-off data steps.
package migration

import (
	"context"
	"fmt"

	"github.com/goakili/akili/server/internal/models"
	"github.com/goakili/akili/server/internal/storage/migration/upgrade"
	"gorm.io/gorm"
)

// Run applies AutoMigrate and every pending upgrade step.
func Run(ctx context.Context, db *gorm.DB) error {
	if err := db.WithContext(ctx).AutoMigrate(models.All()...); err != nil {
		return fmt.Errorf("auto-migrate: %w", err)
	}
	return upgrade.Run(ctx, db)
}
