// SPDX-FileCopyrightText: 2026 Jonas Kaninda
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
