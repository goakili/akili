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
