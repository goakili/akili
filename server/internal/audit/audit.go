// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package audit writes the append-only, hash-chained audit trail.
//
// Each row's hash covers its content and the previous row's hash, so editing or deleting any row
// breaks every later hash. Writes are serialised with a Postgres advisory lock so the chain has a
// single order even with several control-plane replicas.
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/goakili/akili/server/internal/crypto"
	"github.com/goakili/akili/server/internal/models"
	"github.com/jkaninda/logger"
	"gorm.io/gorm"
)

// Actor types.
const (
	ActorUser   = "user"
	ActorAgent  = "agent"
	ActorSystem = "system"
)

// Entry is one audited action.
type Entry struct {
	OrganizationID string
	ActorType      string
	ActorID        string
	Action         string
	TargetType     string
	TargetID       string
	IP             string
	Metadata       map[string]any
}

// Logger records audit entries.
type Logger struct {
	db *gorm.DB
}

// New returns an audit logger.
func New(db *gorm.DB) *Logger { return &Logger{db: db} }

// advisoryKey is an arbitrary constant naming the audit-chain lock.
const advisoryKey = 0x616b696c69 // "akili"

// Record appends an entry. Callers on high-risk paths must treat an error as "do not proceed".
func (l *Logger) Record(ctx context.Context, e Entry) error {
	if e.OrganizationID == "" || e.Action == "" {
		return errors.New("audit: organization and action are required")
	}
	return l.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", advisoryKey).Error; err != nil {
			return err
		}
		var prev models.AuditLog
		prevHash := ""
		if err := tx.Select("hash").Order("id DESC").Limit(1).Find(&prev).Error; err != nil {
			return err
		}
		prevHash = prev.Hash
		row := models.AuditLog{
			OrganizationID: e.OrganizationID,
			// Postgres stores microseconds; truncate before hashing so verification recomputes the same value.
			CreatedAt:  time.Now().UTC().Truncate(time.Microsecond),
			ActorType:  e.ActorType,
			ActorID:    e.ActorID,
			Action:     e.Action,
			TargetType: e.TargetType,
			TargetID:   e.TargetID,
			IP:         e.IP,
			Metadata:   normalize(e.Metadata),
			PrevHash:   prevHash,
		}
		h, err := hashRow(&row)
		if err != nil {
			return err
		}
		row.Hash = h
		return tx.Create(&row).Error
	})
}

// Best records and logs a failure instead of returning it, for low-risk paths (reads, logins) where
// an audit outage must not take the UI down.
func (l *Logger) Best(ctx context.Context, e Entry) {
	// The action already happened; its record must not be lost because the request ended.
	if err := l.Record(context.WithoutCancel(ctx), e); err != nil {
		logger.Error("audit write failed", "action", e.Action, "error", err)
	}
}

// normalize round-trips metadata through JSON so the hashed form equals what jsonb returns.
func normalize(m map[string]any) map[string]any {
	if len(m) == 0 {
		return map[string]any{}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return map[string]any{"_error": "unserialisable metadata"}
	}
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

func hashRow(r *models.AuditLog) (string, error) {
	meta, err := json.Marshal(r.Metadata) // map keys marshal sorted: deterministic
	if err != nil {
		return "", err
	}
	s := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%s|%s|%s", r.PrevHash, r.OrganizationID,
		r.CreatedAt.UTC().Format(time.RFC3339Nano), r.ActorType, r.ActorID, r.Action, r.TargetType, r.TargetID, r.IP, meta)
	return crypto.SHA256Hex([]byte(s)), nil
}

// VerifyResult reports a chain check.
type VerifyResult struct {
	Valid    bool   `json:"valid"`
	Checked  int    `json:"checked"`
	BrokenAt uint   `json:"broken_at,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// Verify walks the whole chain in order.
func (l *Logger) Verify(ctx context.Context) (VerifyResult, error) {
	res := VerifyResult{Valid: true}
	prev := ""
	var batch []models.AuditLog
	err := l.db.WithContext(ctx).Order("id").FindInBatches(&batch, 1000, func(tx *gorm.DB, _ int) error {
		for i := range batch {
			r := &batch[i]
			res.Checked++
			if r.PrevHash != prev {
				res.Valid, res.BrokenAt, res.Reason = false, r.ID, "previous-hash link does not match"
				return errStop
			}
			h, err := hashRow(r)
			if err != nil {
				return err
			}
			if h != r.Hash {
				res.Valid, res.BrokenAt, res.Reason = false, r.ID, "row content does not match its hash"
				return errStop
			}
			prev = r.Hash
		}
		return nil
	}).Error
	if err != nil && !errors.Is(err, errStop) {
		return res, err
	}
	return res, nil
}

var errStop = errors.New("stop")
