// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package siem forwards the audit trail to external systems (SIEM sinks).
//
// The leader replica tails audit_logs by id from a cursor stored per sink, so delivery is
// at-least-once and survives replica restarts and sink outages: a sink that was down catches up.
// Every event carries its hash and previous hash, so the receiver can verify the chain itself.
package siem

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/goakili/akili/server/internal/models"
	"github.com/jkaninda/logger"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Event is one audit row as sent to a sink.
type Event struct {
	Source string `json:"source"`
	models.AuditLog
}

// Sink delivers a batch of events, all or nothing.
type Sink interface {
	Name() string
	Send(ctx context.Context, events []Event) error
}

// Leader reports whether this replica runs singleton loops.
type Leader interface{ Leading() bool }

// Status is a sink's delivery state, stored with its cursor.
type Status struct {
	Sink       string     `json:"sink"`
	Cursor     uint       `json:"cursor"`
	Lag        int64      `json:"lag"`
	LastSentAt *time.Time `json:"last_sent_at,omitempty"`
	LastError  string     `json:"last_error,omitempty"`
	ErrorAt    *time.Time `json:"error_at,omitempty"`
}

// Forwarder moves audit rows to the configured sinks.
type Forwarder struct {
	db        *gorm.DB
	sinks     []Sink
	leader    Leader
	batch     int
	interval  time.Duration
	fromStart bool
}

// New returns a forwarder. fromStart sends the whole existing trail to a new sink; otherwise a new
// sink starts at the current end.
func New(db *gorm.DB, leader Leader, fromStart bool, sinks ...Sink) *Forwarder {
	return &Forwarder{db: db, sinks: sinks, leader: leader, batch: 500, interval: 2 * time.Second, fromStart: fromStart}
}

// Enabled reports whether any sink is configured.
func (f *Forwarder) Enabled() bool { return len(f.sinks) > 0 }

func settingKey(sink string) string { return "siem.cursor." + sink }

// Run forwards until ctx ends. Only the leader sends, so rows are not delivered once per replica.
func (f *Forwarder) Run(ctx context.Context) {
	if !f.Enabled() {
		return
	}
	t := time.NewTicker(f.interval)
	defer t.Stop()
	retryAt := map[string]time.Time{}
	backoff := map[string]time.Duration{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if !f.leader.Leading() {
			continue
		}
		for _, s := range f.sinks {
			if time.Now().Before(retryAt[s.Name()]) {
				continue
			}
			if err := f.drain(ctx, s); err != nil {
				b := min(max(backoff[s.Name()]*2, 5*time.Second), 5*time.Minute)
				backoff[s.Name()], retryAt[s.Name()] = b, time.Now().Add(b)
				logger.Warn("siem sink delivery failed", "sink", s.Name(), "error", err, "retry_in", b)
				continue
			}
			backoff[s.Name()] = 0
		}
	}
}

// drain sends batches until the sink is caught up.
func (f *Forwarder) drain(ctx context.Context, s Sink) error {
	st, err := f.load(ctx, s.Name())
	if err != nil {
		return err
	}
	for ctx.Err() == nil {
		var rows []models.AuditLog
		if err := f.db.WithContext(ctx).Where("id > ?", st.Cursor).Order("id").Limit(f.batch).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		events := make([]Event, len(rows))
		for i := range rows {
			events[i] = Event{Source: "akili", AuditLog: rows[i]}
		}
		sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := s.Send(sendCtx, events)
		cancel()
		now := time.Now().UTC()
		if err != nil {
			st.LastError, st.ErrorAt = err.Error(), &now
			_ = f.save(ctx, st)
			return err
		}
		st.Cursor, st.LastSentAt, st.LastError, st.ErrorAt = rows[len(rows)-1].ID, &now, "", nil
		if err := f.save(ctx, st); err != nil {
			return err
		}
		if len(rows) < f.batch {
			return nil
		}
	}
	return ctx.Err()
}

func (f *Forwarder) load(ctx context.Context, sink string) (*Status, error) {
	var row models.Setting
	err := f.db.WithContext(ctx).First(&row, "key = ?", settingKey(sink)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		st := &Status{Sink: sink}
		if !f.fromStart {
			var last models.AuditLog
			f.db.WithContext(ctx).Select("id").Order("id DESC").Limit(1).Find(&last)
			st.Cursor = last.ID
		}
		return st, f.save(ctx, st)
	}
	if err != nil {
		return nil, err
	}
	st := &Status{Sink: sink}
	return st, json.Unmarshal([]byte(row.Value), st)
}

func (f *Forwarder) save(ctx context.Context, st *Status) error {
	b, _ := json.Marshal(st)
	return f.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"})}).
		Create(&models.Setting{Key: settingKey(st.Sink), Value: string(b), UpdatedAt: time.Now().UTC()}).Error
}

// Statuses reports each configured sink's cursor, lag and last error.
func (f *Forwarder) Statuses(ctx context.Context) []Status {
	var last models.AuditLog
	f.db.WithContext(ctx).Select("id").Order("id DESC").Limit(1).Find(&last)
	out := make([]Status, 0, len(f.sinks))
	for _, s := range f.sinks {
		st := Status{Sink: s.Name()}
		var row models.Setting
		if f.db.WithContext(ctx).First(&row, "key = ?", settingKey(s.Name())).Error == nil {
			_ = json.Unmarshal([]byte(row.Value), &st)
		}
		st.Lag = int64(last.ID) - int64(st.Cursor)
		out = append(out, st)
	}
	return out
}
