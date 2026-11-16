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

package autonomy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/goakili/akili/server/internal/audit"
	"github.com/goakili/akili/server/internal/models"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// ActiveAt reports whether the window covers now; a recurring one is active if it fired within the last DurationMin.
func ActiveAt(w *models.ChangeWindow, now time.Time) bool {
	if !w.Enabled {
		return false
	}
	if w.Cron == "" {
		return w.StartsAt != nil && w.EndsAt != nil && !now.Before(*w.StartsAt) && now.Before(*w.EndsAt)
	}
	loc, err := time.LoadLocation(tzOrUTC(w.Timezone))
	if err != nil || w.DurationMin <= 0 {
		return false
	}
	sched, err := cronParser.Parse(w.Cron)
	if err != nil {
		return false
	}
	start := now.In(loc).Add(-time.Duration(w.DurationMin) * time.Minute)
	return !sched.Next(start).After(now.In(loc))
}

func tzOrUTC(tz string) string {
	if strings.TrimSpace(tz) == "" {
		return "UTC"
	}
	return tz
}

func (s *Service) activeWindows(ctx context.Context, org string, agent *models.Agent, now time.Time) []models.ChangeWindow {
	var all []models.ChangeWindow
	s.db.WithContext(ctx).Where("organization_id = ? AND enabled = ?", org, true).Order("created_at").Find(&all)
	var out []models.ChangeWindow
	for i := range all {
		w := &all[i]
		if (len(w.Labels) == 0 || appliesTo(nil, w.Labels, agent)) && ActiveAt(w, now) {
			out = append(out, *w)
		}
	}
	return out
}

// WindowInput is a recurring window (Cron and DurationMin) or a one-off one (StartsAt and EndsAt).
type WindowInput struct {
	Name        string     `json:"name"`
	Kind        string     `json:"kind"`
	Cron        string     `json:"cron"`
	DurationMin int        `json:"duration_min"`
	StartsAt    *time.Time `json:"starts_at"`
	EndsAt      *time.Time `json:"ends_at"`
	Timezone    string     `json:"timezone"`
	Labels      []string   `json:"labels"`
	Enabled     bool       `json:"enabled"`
}

func (in *WindowInput) validate() error {
	in.Name, in.Cron, in.Timezone = strings.TrimSpace(in.Name), strings.TrimSpace(in.Cron), strings.TrimSpace(in.Timezone)
	switch {
	case in.Name == "":
		return errors.New("a window needs a name")
	case in.Kind != models.WindowKindWindow && in.Kind != models.WindowKindFreeze:
		return errors.New("kind must be window or freeze")
	}
	if _, err := time.LoadLocation(tzOrUTC(in.Timezone)); err != nil {
		return fmt.Errorf("unknown timezone %q", in.Timezone)
	}
	if in.Cron != "" {
		if _, err := cronParser.Parse(in.Cron); err != nil {
			return fmt.Errorf("invalid cron expression: %w", err)
		}
		if in.DurationMin < 1 || in.DurationMin > 7*24*60 {
			return errors.New("a recurring window lasts between 1 minute and 7 days")
		}
		in.StartsAt, in.EndsAt = nil, nil
		return nil
	}
	if in.StartsAt == nil || in.EndsAt == nil || !in.EndsAt.After(*in.StartsAt) {
		return errors.New("give a cron expression and duration, or a start before an end")
	}
	in.DurationMin = 0
	return nil
}

func (s *Service) ListWindows(ctx context.Context, org string) ([]models.ChangeWindow, error) {
	var out []models.ChangeWindow
	if err := s.db.WithContext(ctx).Where("organization_id = ?", org).Order("created_at DESC").Find(&out).Error; err != nil {
		return nil, err
	}
	now := s.now()
	for i := range out {
		out[i].Active = ActiveAt(&out[i], now)
	}
	return out, nil
}

func (s *Service) SaveWindow(ctx context.Context, org, userID, id string, in WindowInput) (*models.ChangeWindow, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	w := models.ChangeWindow{Base: models.Base{ID: models.NewID("win"), OrganizationID: org}, CreatedBy: userID}
	if id != "" {
		w = models.ChangeWindow{}
		if err := s.db.WithContext(ctx).First(&w, "id = ? AND organization_id = ?", id, org).Error; err != nil {
			return nil, err
		}
	}
	w.Name, w.Kind, w.Cron, w.DurationMin, w.StartsAt, w.EndsAt = in.Name, in.Kind, in.Cron, in.DurationMin, in.StartsAt, in.EndsAt
	w.Timezone, w.Labels, w.Enabled = in.Timezone, in.Labels, in.Enabled
	if err := s.db.WithContext(ctx).Save(&w).Error; err != nil {
		return nil, err
	}
	w.Active = ActiveAt(&w, s.now())
	s.audit.Best(ctx, audit.Entry{OrganizationID: org, ActorType: audit.ActorUser, ActorID: userID, Action: "window.saved",
		TargetType: "change_window", TargetID: w.ID, Metadata: map[string]any{"name": w.Name, "kind": w.Kind, "cron": w.Cron,
			"duration_min": w.DurationMin, "starts_at": w.StartsAt, "ends_at": w.EndsAt, "timezone": w.Timezone, "labels": w.Labels, "enabled": w.Enabled}})
	return &w, nil
}

func (s *Service) DeleteWindow(ctx context.Context, org, userID, id string) error {
	res := s.db.WithContext(ctx).Where("id = ? AND organization_id = ?", id, org).Delete(&models.ChangeWindow{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	s.audit.Best(ctx, audit.Entry{OrganizationID: org, ActorType: audit.ActorUser, ActorID: userID, Action: "window.deleted", TargetType: "change_window", TargetID: id})
	return nil
}
