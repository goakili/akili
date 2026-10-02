// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package upgrade runs one-off data-upgrade steps: backfills, renames and other changes AutoMigrate
// cannot express. Schema changes belong on the models; this package is only for data.
//
// Each step lives in its own file, steps_YYYY_MM_DD_<name>.go, named for the day it is added, and
// registers itself from init. Go runs init functions in filename order, so the date prefix is what
// orders the steps. A step runs once, in one transaction with its record in upgrade_steps, so a
// step that fails leaves nothing behind and runs again on the next start. Never edit or remove a
// released step; add a new one.
//
// A new step, in steps_2026_11_02_agent_label_case.go:
//
//	// agentLabelCase lowercases agent labels, which are now matched case-sensitively.
//	func agentLabelCase(ctx context.Context, tx *gorm.DB) error {
//		return tx.WithContext(ctx).Exec(`UPDATE agents SET labels = lower(labels::text)::jsonb`).Error
//	}
//
//	func init() {
//		register(Step{Name: "agent_label_case", Version: "0.1.0", Run: agentLabelCase})
//	}
package upgrade

import (
	"context"
	"fmt"
	"time"

	"github.com/goakili/akili/server/internal/models"
	"github.com/jkaninda/logger"
	"gorm.io/gorm"
)

// Step is a single data-upgrade step.
type Step struct {
	// Name identifies the step in upgrade_steps; it never changes once released.
	Name string
	// Version is the Akili release that introduced the step.
	Version string
	// Run transforms the data. tx is the step's transaction.
	Run func(ctx context.Context, tx *gorm.DB) error
}

// steps is the ordered registry, filled by each step file's init.
var steps []Step

func register(s Step) { steps = append(steps, s) }

// Run applies every step not yet recorded, in order.
func Run(ctx context.Context, db *gorm.DB) error {
	return run(ctx, db, steps)
}

func run(ctx context.Context, db *gorm.DB, list []Step) error {
	seen := make(map[string]bool, len(list))
	for _, s := range list {
		if s.Name == "" || seen[s.Name] {
			return fmt.Errorf("upgrade step %q: the name is empty or registered twice", s.Name)
		}
		seen[s.Name] = true
	}
	for _, s := range list {
		var n int64
		if err := db.WithContext(ctx).Model(&models.UpgradeStep{}).Where("name = ?", s.Name).Count(&n).Error; err != nil {
			return fmt.Errorf("upgrade step %q: %w", s.Name, err)
		}
		if n > 0 {
			continue
		}
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := s.Run(ctx, tx); err != nil {
				return err
			}
			return tx.Create(&models.UpgradeStep{Name: s.Name, Version: s.Version, AppliedAt: time.Now().UTC()}).Error
		})
		if err != nil {
			return fmt.Errorf("upgrade step %q: %w", s.Name, err)
		}
		logger.Info("applied upgrade step", "name", s.Name, "version", s.Version)
	}
	return nil
}
