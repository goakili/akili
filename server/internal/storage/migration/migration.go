// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package migration evolves the database schema with GORM: AutoMigrate for additive changes, then
// ordered, recorded steps for anything AutoMigrate cannot express (data fixes, renames, backfills).
package migration

import (
	"fmt"
	"time"

	"github.com/goakili/akili/server/internal/models"
	"github.com/jkaninda/logger"
	"gorm.io/gorm"
)

// Step is a one-time migration. IDs are dated ("2026_09_30_add_x") and never reused or reordered;
// each runs once, in its own transaction, and is recorded in schema_migrations.
type Step struct {
	ID string
	Up func(tx *gorm.DB) error
}

// steps lists versioned migrations in order. Append new steps; never edit an applied one.
var steps = []Step{
	{
		ID: "2026_09_30_project_forge_kind",
		Up: func(tx *gorm.DB) error {
			var projects []models.Project
			if err := tx.Where("forge = '' OR forge IS NULL").Find(&projects).Error; err != nil {
				return err
			}
			for _, p := range projects {
				var it models.Integration
				if tx.Select("kind").First(&it, "id = ?", p.IntegrationID).Error == nil {
					if err := tx.Model(&models.Project{}).Where("id = ?", p.ID).Update("forge", it.Kind).Error; err != nil {
						return err
					}
				}
			}
			return nil
		},
	},
	{
		// Watches predate multi-workspace integrations: they watched the integration's one workspace.
		// The value may be an id or uid; the first workspace sync rewrites it to the handle.
		ID: "2026_10_01_miabi_watch_workspace",
		Up: func(tx *gorm.DB) error {
			var watches []models.MiabiWatch
			if err := tx.Where("workspace = '' OR workspace IS NULL").Find(&watches).Error; err != nil {
				return err
			}
			for _, w := range watches {
				var it models.Integration
				if tx.Select("workspace").First(&it, "id = ?", w.IntegrationID).Error == nil {
					if err := tx.Model(&models.MiabiWatch{}).Where("id = ?", w.ID).Updates(map[string]any{"workspace": it.Workspace, "triage_failures": w.TriageFailures}).Error; err != nil {
						return err
					}
				}
			}
			return nil
		},
	},
}

// Run applies AutoMigrate and every pending step.
func Run(db *gorm.DB) error {
	if err := db.AutoMigrate(models.All()...); err != nil {
		return fmt.Errorf("auto-migrate: %w", err)
	}
	for _, s := range steps {
		var n int64
		if err := db.Model(&models.SchemaMigration{}).Where("id = ?", s.ID).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := s.Up(tx); err != nil {
				return err
			}
			return tx.Create(&models.SchemaMigration{ID: s.ID, AppliedAt: time.Now().UTC()}).Error
		})
		if err != nil {
			return fmt.Errorf("migration %s: %w", s.ID, err)
		}
		logger.Info("applied migration", "id", s.ID)
	}
	return nil
}
