// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package fleet manages agents: creation, enrollment, connection authentication and the live
// tunnels to connected agents.
package fleet

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/audit"
	"github.com/goakili/akili/server/internal/bus"
	"github.com/goakili/akili/server/internal/config"
	"github.com/goakili/akili/server/internal/crypto"
	"github.com/goakili/akili/server/internal/gitid"
	"github.com/goakili/akili/server/internal/models"
	"gorm.io/gorm"
)

// DefaultPolicy is bound to new agents created without one.
const DefaultPolicy = "read-only"

// JoinTokenTTL bounds how long an unused enrollment token is valid.
const JoinTokenTTL = time.Hour

// Clock skew tolerated on the signed connect handshake.
const handshakeWindow = 60 * time.Second

// Service is the agent registry.
type Service struct {
	db        *gorm.DB
	bus       *bus.Bus
	audit     *audit.Logger
	cpKey     ed25519.PublicKey
	publicURL string
	git       gitid.Config
}

// NewService returns the registry.
func NewService(db *gorm.DB, b *bus.Bus, a *audit.Logger, cpKey ed25519.PublicKey, publicURL string, git gitid.Config) *Service {
	return &Service{db: db, bus: b, audit: a, cpKey: cpKey, publicURL: publicURL, git: git}
}

// Errors.
var (
	ErrNotFound     = errors.New("agent not found")
	ErrBadToken     = errors.New("join token is invalid, already used or expired (tokens are single-use and valid for 1 hour); an enrolled agent reconnects with its key and needs no token — to enroll again, use Re-enroll on the agent page")
	ErrUnauthorized = errors.New("agent authentication failed")
)

// Input holds editable agent fields.
type Input struct {
	Name          *string
	Description   *string
	Labels        []string
	PolicyID      *string
	Autonomy      *proto.Autonomy
	ProviderID    *string
	MaxParallel   *int
	Instructions  *string
	MonthlyBudget *float64
	GitName       *string
	GitEmail      *string
	SkillIDs      []string
	SetSkills     bool
}

// Enrollment is returned when an agent is created or given a new join token. The token is shown
// once.
type Enrollment struct {
	Agent          *models.Agent `json:"agent"`
	JoinToken      string        `json:"join_token"`
	ExpiresAt      time.Time     `json:"expires_at"`
	InstallCommand string        `json:"install_command"`
	DockerCommand  string        `json:"docker_command"`
}

// Create registers a pending agent and issues its join token.
func (s *Service) Create(ctx context.Context, org, userID string, in Input) (*Enrollment, error) {
	if in.Name == nil || strings.TrimSpace(*in.Name) == "" {
		return nil, errors.New("name is required")
	}
	a := &models.Agent{Base: models.Base{ID: models.NewID("ag"), OrganizationID: org}, Status: models.AgentPending,
		MaxParallel: 2, Autonomy: proto.AutonomyL1, CreatedBy: userID, Labels: []string{}}
	if err := s.apply(ctx, org, a, in); err != nil {
		return nil, err
	}
	if a.PolicyID == nil {
		// Without a policy an agent can use no tools at all; start from the safest template instead.
		var p models.Policy
		if err := s.db.WithContext(ctx).Where("organization_id = ? AND builtin AND name = ?", org, DefaultPolicy).First(&p).Error; err == nil {
			a.PolicyID = &p.ID
		}
	}
	var enr *Enrollment
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(a).Error; err != nil {
			return err
		}
		if in.SetSkills {
			if err := s.setSkills(tx, org, a, in.SkillIDs); err != nil {
				return err
			}
		}
		var err error
		enr, err = s.issueToken(tx, a, userID)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.audit.Best(ctx, audit.Entry{OrganizationID: org, ActorType: audit.ActorUser, ActorID: userID, Action: "agent.create",
		TargetType: "agent", TargetID: a.ID, Metadata: map[string]any{"name": a.Name, "policy_id": a.PolicyID, "autonomy": int(a.Autonomy)}})
	return enr, nil
}

// Update edits an agent.
func (s *Service) Update(ctx context.Context, org, userID, id string, in Input) (*models.Agent, error) {
	a, err := s.Get(ctx, org, id)
	if err != nil {
		return nil, err
	}
	before := s.git.For(a)
	if err := s.apply(ctx, org, a, in); err != nil {
		return nil, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Omit("Skills").Save(a).Error; err != nil {
			return err
		}
		if in.SetSkills {
			return s.setSkills(tx, org, a, in.SkillIDs)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	meta := map[string]any{"policy_id": a.PolicyID, "autonomy": int(a.Autonomy), "labels": a.Labels}
	if after := s.git.For(a); after != before {
		meta["git_identity"] = map[string]string{"from": before.String(), "to": after.String()}
	}
	s.audit.Best(ctx, audit.Entry{OrganizationID: org, ActorType: audit.ActorUser, ActorID: userID, Action: "agent.update",
		TargetType: "agent", TargetID: a.ID, Metadata: meta})
	return s.Get(ctx, org, id)
}

func (s *Service) apply(ctx context.Context, org string, a *models.Agent, in Input) error {
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" || len(name) > 120 {
			return errors.New("name must be 1-120 characters")
		}
		var n int64
		s.db.WithContext(ctx).Model(&models.Agent{}).Where("organization_id = ? AND name = ? AND id <> ?", org, name, a.ID).Count(&n)
		if n > 0 {
			return fmt.Errorf("an agent named %q already exists", name)
		}
		a.Name = name
	}
	if in.Description != nil {
		a.Description = *in.Description
	}
	if in.Labels != nil {
		a.Labels = in.Labels
	}
	if in.PolicyID != nil {
		if *in.PolicyID == "" {
			a.PolicyID = nil
		} else {
			var n int64
			s.db.WithContext(ctx).Model(&models.Policy{}).Where("id = ? AND organization_id = ?", *in.PolicyID, org).Count(&n)
			if n == 0 {
				return errors.New("unknown policy")
			}
			a.PolicyID = in.PolicyID
		}
	}
	if in.ProviderID != nil {
		if *in.ProviderID == "" {
			a.ProviderID = nil
		} else {
			var n int64
			s.db.WithContext(ctx).Model(&models.ModelProvider{}).Where("id = ? AND organization_id = ?", *in.ProviderID, org).Count(&n)
			if n == 0 {
				return errors.New("unknown model provider")
			}
			a.ProviderID = in.ProviderID
		}
	}
	if in.Autonomy != nil {
		if !in.Autonomy.Valid() {
			return errors.New("autonomy must be 0-3")
		}
		a.Autonomy = *in.Autonomy
	}
	if in.MaxParallel != nil {
		if *in.MaxParallel < 1 || *in.MaxParallel > 32 {
			return errors.New("max_parallel must be 1-32")
		}
		a.MaxParallel = *in.MaxParallel
	}
	if in.Instructions != nil {
		a.Instructions = *in.Instructions
	}
	if in.MonthlyBudget != nil {
		if *in.MonthlyBudget < 0 {
			return errors.New("monthly budget cannot be negative")
		}
		a.MonthlyBudget = *in.MonthlyBudget
	}
	if in.GitName != nil {
		if *in.GitName != "" {
			if err := gitid.ValidateName(*in.GitName); err != nil {
				return err
			}
		}
		a.GitName = *in.GitName
	}
	if in.GitEmail != nil {
		if err := s.checkGitEmail(ctx, org, *in.GitEmail); err != nil {
			return err
		}
		a.GitEmail = *in.GitEmail
	}
	return nil
}

// checkGitEmail refuses a member's address: forges credit commits to whoever owns the author
// email, so the agent's commits would show up as that person's.
func (s *Service) checkGitEmail(ctx context.Context, org, email string) error {
	if email == "" {
		return nil
	}
	if err := gitid.ValidateEmail(email, s.git.AllowedDomains); err != nil {
		return err
	}
	var n int64
	s.db.WithContext(ctx).Model(&models.User{}).Where("organization_id = ? AND lower(email) = lower(?)", org, email).Count(&n)
	if n > 0 {
		return errors.New("git email belongs to a user of this organization; agents cannot commit as a person")
	}
	return nil
}

func (s *Service) setSkills(tx *gorm.DB, org string, a *models.Agent, ids []string) error {
	var skills []models.Skill
	if len(ids) > 0 {
		if err := tx.Where("organization_id = ? AND id IN ?", org, ids).Find(&skills).Error; err != nil {
			return err
		}
		if len(skills) != len(ids) {
			return errors.New("unknown skill")
		}
	}
	return tx.Model(a).Association("Skills").Replace(skills)
}

func (s *Service) issueToken(tx *gorm.DB, a *models.Agent, userID string) (*Enrollment, error) {
	token := crypto.NewToken("akj")
	jt := models.JoinToken{Base: models.Base{ID: models.NewID("jt"), OrganizationID: a.OrganizationID}, AgentID: a.ID,
		Hash: crypto.HashToken(token), ExpiresAt: time.Now().UTC().Add(JoinTokenTTL), CreatedBy: userID}
	if err := tx.Create(&jt).Error; err != nil {
		return nil, err
	}
	return &Enrollment{
		Agent: a, JoinToken: token, ExpiresAt: jt.ExpiresAt,
		InstallCommand: fmt.Sprintf("curl -fsSL %s/install-agent.sh | sudo AKILI_URL=%s AKILI_JOIN_TOKEN=%s sh", s.publicURL, s.publicURL, token),
		DockerCommand: fmt.Sprintf("docker run -d --name akili-agent --restart unless-stopped -v akili-agent:/var/lib/akili-agent "+
			"-e AKILI_URL=%s -e AKILI_JOIN_TOKEN=%s jkaninda/akili-agent:%s", s.publicURL, token, config.AgentVersion()),
	}, nil
}

// Reenroll issues a new join token. The current key is revoked immediately, so a stolen agent key
// stops working the moment an operator starts re-enrollment.
func (s *Service) Reenroll(ctx context.Context, org, userID, id string) (*Enrollment, error) {
	a, err := s.Get(ctx, org, id)
	if err != nil {
		return nil, err
	}
	var enr *Enrollment
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		tx.Model(&models.JoinToken{}).Where("agent_id = ? AND used_at IS NULL", a.ID).Update("expires_at", time.Now().UTC())
		if err := tx.Model(a).Updates(map[string]any{"public_key": nil, "status": models.AgentPending, "revoked_at": nil}).Error; err != nil {
			return err
		}
		a.Status = models.AgentPending
		enr, err = s.issueToken(tx, a, userID)
		return err
	})
	if err != nil {
		return nil, err
	}
	_ = s.bus.SendCommand(ctx, a.ID, bus.Command{Type: bus.CmdDisconnect})
	s.audit.Best(ctx, audit.Entry{OrganizationID: org, ActorType: audit.ActorUser, ActorID: userID, Action: "agent.reenroll", TargetType: "agent", TargetID: a.ID})
	return enr, nil
}

// Revoke permanently disables an agent and cuts its tunnel.
func (s *Service) Revoke(ctx context.Context, org, userID, id string) error {
	a, err := s.Get(ctx, org, id)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if err := s.db.WithContext(ctx).Model(a).Updates(map[string]any{"status": models.AgentRevoked, "revoked_at": now, "public_key": nil}).Error; err != nil {
		return err
	}
	_ = s.bus.SendCommand(ctx, a.ID, bus.Command{Type: bus.CmdDisconnect})
	s.bus.ClearPresence(ctx, a.ID)
	if err := s.audit.Record(ctx, audit.Entry{OrganizationID: org, ActorType: audit.ActorUser, ActorID: userID, Action: "agent.revoke", TargetType: "agent", TargetID: a.ID}); err != nil {
		return err
	}
	s.bus.EmitData(ctx, org, bus.Event{Type: "agent.status", AgentID: a.ID}, map[string]string{"status": models.AgentRevoked})
	return nil
}

// SetDraining stops (or resumes) scheduling new tasks onto an agent.
func (s *Service) SetDraining(ctx context.Context, org, userID, id string, draining bool) error {
	a, err := s.Get(ctx, org, id)
	if err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Model(a).Update("draining", draining).Error; err != nil {
		return err
	}
	_ = s.bus.SendCommandData(ctx, a.ID, bus.Command{Type: bus.CmdDrain}, map[string]bool{"draining": draining})
	s.audit.Best(ctx, audit.Entry{OrganizationID: org, ActorType: audit.ActorUser, ActorID: userID, Action: "agent.drain",
		TargetType: "agent", TargetID: a.ID, Metadata: map[string]any{"draining": draining}})
	return nil
}

// Delete removes an agent record (after revoking it).
func (s *Service) Delete(ctx context.Context, org, userID, id string) error {
	if err := s.Revoke(ctx, org, userID, id); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Select("Skills").Delete(&models.Agent{Base: models.Base{ID: id}}).Error
}

// Get loads an agent with its skills.
func (s *Service) Get(ctx context.Context, org, id string) (*models.Agent, error) {
	var a models.Agent
	if err := s.db.WithContext(ctx).Preload("Skills").First(&a, "id = ? AND organization_id = ?", id, org).Error; err != nil {
		return nil, ErrNotFound
	}
	s.annotate(ctx, &a)
	git := s.git.For(&a)
	a.GitIdentity = &git
	return &a, nil
}

// List returns every agent in the organization. Presence and active sessions are fetched in one
// round trip each, not per agent: fleets run to thousands of agents.
func (s *Service) List(ctx context.Context, org string) ([]models.Agent, error) {
	var out []models.Agent
	if err := s.db.WithContext(ctx).Preload("Skills").Where("organization_id = ?", org).Order("name").Find(&out).Error; err != nil {
		return nil, err
	}
	ids := make([]string, len(out))
	for i := range out {
		ids[i] = out[i].ID
	}
	present := s.bus.PresentMany(ctx, ids)
	var counts []struct {
		AgentID string
		N       int
	}
	s.db.WithContext(ctx).Model(&models.ChatSession{}).Select("agent_id, count(*) as n").
		Where("organization_id = ? AND status = ? AND state <> ?", org, models.SessionOpen, proto.StateIdle).Group("agent_id").Scan(&counts)
	active := make(map[string]int, len(counts))
	for _, c := range counts {
		active[c.AgentID] = c.N
	}
	for i := range out {
		a := &out[i]
		if a.Status == models.AgentOnline && !present[a.ID] {
			a.Status = models.AgentOffline
		}
		a.ActiveSessions = active[a.ID]
	}
	return out, nil
}

// annotate corrects a stale "online" row (a replica that died without cleanup) using live presence.
func (s *Service) annotate(ctx context.Context, a *models.Agent) {
	if a.Status == models.AgentOnline && !s.bus.Present(ctx, a.ID) {
		a.Status = models.AgentOffline
	}
	var n int64
	s.db.WithContext(ctx).Model(&models.ChatSession{}).Where("agent_id = ? AND status = ? AND state <> ?", a.ID, models.SessionOpen, proto.StateIdle).Count(&n)
	a.ActiveSessions = int(n)
}

// Enroll binds an agent's public key using a one-time join token.
func (s *Service) Enroll(ctx context.Context, req proto.EnrollRequest, ip string) (*proto.EnrollResponse, error) {
	if len(req.PublicKey) != ed25519.PublicKeySize {
		return nil, errors.New("public key must be a 32-byte Ed25519 key")
	}
	var resp *proto.EnrollResponse
	var agent models.Agent
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var jt models.JoinToken
		if err := tx.Where("hash = ?", crypto.HashToken(req.JoinToken)).First(&jt).Error; err != nil {
			return ErrBadToken
		}
		now := time.Now().UTC()
		// Burn the token atomically: a leaked token replayed concurrently enrolls at most once.
		res := tx.Model(&models.JoinToken{}).Where("id = ? AND used_at IS NULL AND expires_at > ?", jt.ID, now).Update("used_at", now)
		if res.Error != nil || res.RowsAffected == 0 {
			return ErrBadToken
		}
		if err := tx.First(&agent, "id = ?", jt.AgentID).Error; err != nil {
			return ErrBadToken
		}
		if agent.Status == models.AgentRevoked {
			return ErrBadToken
		}
		if err := tx.Model(&agent).Select("public_key", "enrolled_at", "status", "facts", "version").Updates(&models.Agent{
			PublicKey: req.PublicKey, EnrolledAt: &now, Status: models.AgentOffline, Facts: req.Facts, Version: req.Facts.AgentVersion}).Error; err != nil {
			return err
		}
		resp = &proto.EnrollResponse{AgentID: agent.ID, Name: agent.Name, CPSigningKey: s.cpKey, ConnectPath: proto.ConnectPath,
			EnrolledAtUTC: now.Format(time.RFC3339)}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.audit.Best(ctx, audit.Entry{OrganizationID: agent.OrganizationID, ActorType: audit.ActorAgent, ActorID: agent.ID,
		Action: "agent.enroll", TargetType: "agent", TargetID: agent.ID, IP: ip, Metadata: map[string]any{
			"hostname": req.Facts.Hostname, "public_key": base64.StdEncoding.EncodeToString(req.PublicKey)}})
	s.bus.EmitData(ctx, agent.OrganizationID, bus.Event{Type: "agent.status", AgentID: agent.ID}, map[string]string{"status": models.AgentOffline})
	return resp, nil
}

// AuthenticateConnect verifies the signed handshake on the connect request. Refusals carry a reason,
// logged and audited so an operator can see why an agent cannot connect; the agent itself only gets
// a bare 401.
func (s *Service) AuthenticateConnect(ctx context.Context, h http.Header, ip string) (*models.Agent, error) {
	a, err := s.authenticateConnect(ctx, h)
	if err != nil {
		if a != nil {
			s.audit.Best(ctx, audit.Entry{OrganizationID: a.OrganizationID, ActorType: audit.ActorAgent, ActorID: a.ID,
				Action: "agent.connect_refused", TargetType: "agent", TargetID: a.ID, IP: ip, Metadata: map[string]any{"reason": err.Error()}})
		}
		return nil, err
	}
	return a, nil
}

// authenticateConnect returns the agent it identified (when it got that far) alongside any error.
func (s *Service) authenticateConnect(ctx context.Context, h http.Header) (*models.Agent, error) {
	id := h.Get(proto.HeaderAgentID)
	ts := h.Get(proto.HeaderTimestamp)
	nonce := h.Get(proto.HeaderNonce)
	sig, err := base64.StdEncoding.DecodeString(h.Get(proto.HeaderSignature))
	if id == "" || ts == "" || len(nonce) < 16 || err != nil {
		return nil, fmt.Errorf("%w: malformed handshake", ErrUnauthorized)
	}
	var a models.Agent
	if err := s.db.WithContext(ctx).First(&a, "id = ?", id).Error; err != nil {
		return nil, fmt.Errorf("%w: unknown agent %s (deleted, or enrolled against another database)", ErrUnauthorized, id)
	}
	switch {
	case a.Status == models.AgentRevoked:
		return &a, fmt.Errorf("%w: agent is revoked", ErrUnauthorized)
	case a.Status == models.AgentPending || len(a.PublicKey) != ed25519.PublicKeySize:
		return &a, fmt.Errorf("%w: agent is waiting for re-enrollment; its previous key was revoked", ErrUnauthorized)
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return &a, fmt.Errorf("%w: malformed timestamp", ErrUnauthorized)
	}
	if d := time.Since(time.Unix(sec, 0)); d > handshakeWindow || d < -handshakeWindow {
		return &a, fmt.Errorf("%w: clock skew of %s (check NTP on the agent host)", ErrUnauthorized, d.Round(time.Second))
	}
	if !ed25519.Verify(a.PublicKey, proto.HandshakeMessage(id, ts, nonce), sig) {
		return &a, fmt.Errorf("%w: signature does not match the enrolled key (the agent was re-enrolled elsewhere, or its state is stale)", ErrUnauthorized)
	}
	if !s.bus.UseNonce(ctx, id, nonce, 2*handshakeWindow) {
		return &a, fmt.Errorf("%w: replayed handshake", ErrUnauthorized)
	}
	return &a, nil
}
