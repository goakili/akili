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

// Package upgrade runs one-off data steps: add steps_YYYY_MM_DD_<name>.go registering a Step from init; never edit a released step.
package upgrade

import (
	"context"
	"fmt"
	"time"

	"github.com/goakili/akili/server/internal/models"
	"github.com/jkaninda/logger"
	"gorm.io/gorm"
)

type Step struct {
	// Name identifies the step in upgrade_steps; it never changes once released.
	Name string
	// Version is the Akili release that introduced the step.
	Version string
	// Run transforms the data. tx is the step's transaction.
	Run func(ctx context.Context, tx *gorm.DB) error
}

// steps is filled by each step file's init, in filename (date) order.
var steps []Step

func register(s Step) { steps = append(steps, s) }

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
