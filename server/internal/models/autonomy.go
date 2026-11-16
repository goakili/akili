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

package models

import (
	"encoding/json"
	"time"
)

// AutonomySettings are per-organization limits; a zero field takes its default.
type AutonomySettings struct {
	GrantMaxHours       int `json:"grant_max_hours"`
	StopRepeatFailures  int `json:"stop_repeat_failures"`
	StopErrorStreak     int `json:"stop_error_streak"`
	StopMaxToolCalls    int `json:"stop_max_tool_calls"`
	SuggestMinApprovals int `json:"suggest_min_approvals"`
	SuggestMinApprovers int `json:"suggest_min_approvers"`
	SuggestMinDays      int `json:"suggest_min_days"`
}

// GrantHardMaxHours bounds every grant, whatever the organization setting says.
const GrantHardMaxHours = 7 * 24

func (s AutonomySettings) WithDefaults() AutonomySettings {
	def := func(v *int, d int) {
		if *v <= 0 {
			*v = d
		}
	}
	def(&s.GrantMaxHours, 24)
	def(&s.StopRepeatFailures, 3)
	def(&s.StopErrorStreak, 15)
	def(&s.StopMaxToolCalls, 200)
	def(&s.SuggestMinApprovals, 10)
	def(&s.SuggestMinApprovers, 2)
	def(&s.SuggestMinDays, 7)
	s.GrantMaxHours = min(s.GrantMaxHours, GrantHardMaxHours)
	return s
}

// Constraint bounds one argument: exactly one of Equals, Glob, OneOf, or a Min/Max range.
type Constraint struct {
	Equals json.RawMessage `json:"equals,omitempty"`
	Glob   string          `json:"glob,omitempty"`
	OneOf  []string        `json:"one_of,omitempty"`
	Min    *float64        `json:"min,omitempty"`
	Max    *float64        `json:"max,omitempty"`
}

// ApprovalGrant turns a call that needs approval into an allowed one while it matches; it never touches a denied call.
type ApprovalGrant struct {
	Base
	AgentID *string  `gorm:"size:40;index" json:"agent_id"`
	Labels  []string `gorm:"type:jsonb;serializer:json" json:"labels"`
	Tool    string   `gorm:"size:120;not null" json:"tool"`
	// Match names every top-level argument of the call; a call with an argument not listed here does not match.
	Match       map[string]Constraint `gorm:"type:jsonb;serializer:json" json:"match"`
	RunbookID   *string               `gorm:"size:40;index" json:"runbook_id"`
	RunbookHash string                `gorm:"size:64" json:"runbook_hash"`
	MaxRisk     string                `gorm:"size:20;not null" json:"max_risk"`
	Windowed    bool                  `json:"windowed"`
	ExpiresAt   time.Time             `gorm:"index" json:"expires_at"`
	MaxUses     int                   `json:"max_uses"`
	Uses        int                   `gorm:"not null;default:0" json:"uses"`
	LastUsedAt  *time.Time            `json:"last_used_at"`
	Reason      string                `gorm:"size:500;not null" json:"reason"`
	CreatedBy   string                `gorm:"size:40" json:"created_by"`
	RevokedAt   *time.Time            `json:"revoked_at"`
	RevokedBy   *string               `gorm:"size:40" json:"revoked_by"`
	// ApprovalID is the approval this grant was created from, if any.
	ApprovalID *string `gorm:"size:40" json:"approval_id"`
}

func (g *ApprovalGrant) Active(now time.Time) bool {
	return g.RevokedAt == nil && now.Before(g.ExpiresAt) && (g.MaxUses == 0 || g.Uses < g.MaxUses)
}

const (
	WindowKindWindow = "window"
	WindowKindFreeze = "freeze"
)

// ChangeWindow is a recurring (cron + duration) or one-off period; a freeze forces approval, a window enables windowed grants.
type ChangeWindow struct {
	Base
	Name        string     `gorm:"size:120;not null" json:"name"`
	Kind        string     `gorm:"size:20;not null" json:"kind"`
	Cron        string     `gorm:"size:120" json:"cron"`
	DurationMin int        `json:"duration_min"`
	StartsAt    *time.Time `json:"starts_at"`
	EndsAt      *time.Time `json:"ends_at"`
	Timezone    string     `gorm:"size:60" json:"timezone"`
	// Labels select agents; empty applies to every agent.
	Labels    []string `gorm:"type:jsonb;serializer:json" json:"labels"`
	Enabled   bool     `json:"enabled"`
	CreatedBy string   `gorm:"size:40" json:"created_by"`
	Active    bool     `gorm:"-" json:"active"`
}

// RunbookParam is a "{{name}}" placeholder of a runbook and the values it may take.
type RunbookParam struct {
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Bounds      Constraint `json:"bounds"`
}

// Runbook is a change plan template; Hash covers the calls and parameters, so any edit invalidates grants bound to it.
type Runbook struct {
	Base
	Name           string          `gorm:"size:120;not null" json:"name"`
	Description    string          `gorm:"size:1000" json:"description"`
	Plan           json.RawMessage `gorm:"type:jsonb" json:"plan"`
	Params         []RunbookParam  `gorm:"type:jsonb;serializer:json" json:"params"`
	Hash           string          `gorm:"size:64;not null" json:"hash"`
	Version        int             `gorm:"not null;default:1" json:"version"`
	Enabled        bool            `json:"enabled"`
	SourceChangeID *string         `gorm:"size:40" json:"source_change_id"`
	CreatedBy      string          `gorm:"size:40" json:"created_by"`
}

// SuggestionDismissal hides an autonomy suggestion until the evidence behind it changes.
type SuggestionDismissal struct {
	Base
	AgentID string `gorm:"size:40;index;not null" json:"agent_id"`
	Key     string `gorm:"size:64;index;not null" json:"key"`
	By      string `gorm:"size:40" json:"by"`
}
