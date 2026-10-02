// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package coder is the control-plane side of coding work: forge integrations, projects, the git
// proxy agents clone and push through, and the server-side tools that open and inspect pull
// requests. Forge credentials stay here; agents never see them.
package coder

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/audit"
	"github.com/goakili/akili/server/internal/crypto"
	"github.com/goakili/akili/server/internal/forge"
	"github.com/goakili/akili/server/internal/gitid"
	"github.com/goakili/akili/server/internal/models"
	"gorm.io/gorm"
)

type forgeClient = forge.Forge

// Service is the coder service.
type Service struct {
	db      *gorm.DB
	box     *crypto.Box
	audit   *audit.Logger
	gitHTTP *http.Client
	git     gitid.Config

	mu     sync.Mutex
	forges map[string]cachedForge // by integration id
}

type cachedForge struct {
	updated time.Time
	f       forge.Forge
}

// New returns the coder service.
func New(db *gorm.DB, box *crypto.Box, a *audit.Logger, git gitid.Config) *Service {
	return &Service{db: db, box: box, audit: a, gitHTTP: &http.Client{Timeout: gitClientTimeout}, git: git, forges: map[string]cachedForge{}}
}

// Errors.
var (
	ErrNotFound  = errors.New("not found")
	ErrNoProject = errors.New("this session is not bound to a project")
)

// ---- integrations ----

// IntegrationInput creates or updates an integration. Empty secrets on update keep the old ones.
type IntegrationInput struct {
	Name           string
	Kind           string
	BaseURL        string
	WebURL         string
	AuthType       string
	Username       string
	Token          string
	AppID          int64
	InstallationID int64
	PrivateKey     string
	WebhookSecret  string
	Workspace      string
	CACert         *string // nil keeps the stored CA; "" removes it
	Default        *bool   // Miabi and Posta only; nil keeps the current setting
	Sender         string  // Posta: the From address
}

// hasDefault reports whether a kind has a default integration (used when a call names none).
func hasDefault(kind string) bool { return kind == models.KindMiabi || kind == models.KindPosta }

// SaveIntegration validates, encrypts secrets and stores an integration.
func (s *Service) SaveIntegration(ctx context.Context, it *models.Integration, in IntegrationInput) error {
	switch in.Kind {
	case models.KindMiabi:
		// The workspace is optional: an account-wide key reaches every workspace of its user; the
		// workspaces are discovered and enabled one by one.
		if in.BaseURL == "" {
			return errors.New("a Miabi integration needs base_url")
		}
		in.AuthType = models.AuthToken
	case models.KindPosta:
		if in.BaseURL == "" {
			return errors.New("a Posta integration needs base_url")
		}
		if err := validSender(in.Sender); err != nil {
			return err
		}
		in.AuthType = models.AuthToken
	case models.ForgeGitea:
		if in.BaseURL == "" {
			return errors.New("base_url is required for Gitea")
		}
		in.AuthType = models.AuthToken
	case models.ForgeGitHub:
		if in.BaseURL == "" {
			in.BaseURL = "https://api.github.com"
		}
		if in.WebURL == "" {
			in.WebURL = "https://github.com"
		}
		if in.AuthType == "" {
			in.AuthType = models.AuthToken
		}
	default:
		return errors.New("kind must be gitea, github, miabi or posta")
	}
	if in.AuthType != models.AuthToken && in.AuthType != models.AuthGitHubApp {
		return errors.New("auth_type must be token or github_app")
	}
	if in.AuthType == models.AuthGitHubApp && (in.Kind != models.ForgeGitHub || in.AppID == 0 || in.InstallationID == 0) {
		return errors.New("a GitHub App needs app_id and installation_id")
	}
	it.Name, it.Kind, it.AuthType, it.Username = strings.TrimSpace(in.Name), in.Kind, in.AuthType, strings.TrimSpace(in.Username)
	it.BaseURL, it.WebURL = strings.TrimRight(in.BaseURL, "/"), strings.TrimRight(in.WebURL, "/")
	it.AppID, it.InstallationID = in.AppID, in.InstallationID
	it.Workspace = strings.TrimSpace(in.Workspace)
	it.Sender = strings.TrimSpace(in.Sender)
	if in.CACert != nil {
		ca := strings.TrimSpace(*in.CACert)
		if ca != "" {
			if err := crypto.ValidateCAPEM([]byte(ca)); err != nil {
				return err
			}
		}
		it.CACert = ca
	}
	if it.Name == "" {
		return errors.New("name is required")
	}
	var err error
	if in.Token != "" {
		if it.TokenEnc, err = s.box.Encrypt(in.Token); err != nil {
			return err
		}
	}
	if in.PrivateKey != "" {
		if it.PrivateKeyEnc, err = s.box.Encrypt(in.PrivateKey); err != nil {
			return err
		}
	}
	if in.WebhookSecret != "" {
		if it.WebhookSecretEnc, err = s.box.Encrypt(in.WebhookSecret); err != nil {
			return err
		}
	}
	if it.AuthType == models.AuthToken && it.TokenEnc == "" {
		return errors.New("token is required")
	}
	if it.AuthType == models.AuthGitHubApp && it.PrivateKeyEnc == "" {
		return errors.New("private_key is required for a GitHub App")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if hasDefault(it.Kind) {
			if in.Default != nil {
				it.Default = *in.Default
			} else if it.CreatedAt.IsZero() {
				// The first integration of its kind becomes the default, so a second one never leaves calls ambiguous.
				var n int64
				tx.Model(&models.Integration{}).Where("organization_id = ? AND kind = ? AND is_default = ?", it.OrganizationID, it.Kind, true).Count(&n)
				it.Default = n == 0
			}
		} else {
			it.Default = false
		}
		if err := tx.Save(it).Error; err != nil {
			return err
		}
		if it.Default {
			if err := tx.Model(&models.Integration{}).Where("organization_id = ? AND kind = ? AND id <> ?", it.OrganizationID, it.Kind, it.ID).
				Update("is_default", false).Error; err != nil {
				return err
			}
		}
		it.HasSecret = true
		return nil
	})
}

// validSender accepts "Name <address>" or a bare address, without line breaks (it becomes a header).
func validSender(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return errors.New("a Posta integration needs a sender address")
	}
	if strings.ContainsAny(s, "\r\n") {
		return errors.New("sender must be one line")
	}
	if _, err := mail.ParseAddress(s); err != nil {
		return errors.New(`sender must be an address like "Akili <akili@example.com>"`)
	}
	return nil
}

// ErrNotMiabi refuses a default on a forge integration: only Miabi tool calls and Posta email pick
// one implicitly.
var ErrNotMiabi = errors.New("only a Miabi or Posta integration can be the default")

// SetDefault makes a Miabi or Posta integration the one used when none is named.
func (s *Service) SetDefault(ctx context.Context, org, id string) (*models.Integration, error) {
	var it models.Integration
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&it, "id = ? AND organization_id = ?", id, org).Error; err != nil {
			return err
		}
		if !hasDefault(it.Kind) {
			return ErrNotMiabi
		}
		if err := tx.Model(&models.Integration{}).Where("organization_id = ? AND kind = ?", org, it.Kind).
			Update("is_default", gorm.Expr("id = ?", it.ID)).Error; err != nil {
			return err
		}
		it.Default = true
		return nil
	})
	return &it, err
}

// Forge builds (and caches) the client for an integration.
func (s *Service) Forge(it *models.Integration) (forge.Forge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.forges[it.ID]; ok && c.updated.Equal(it.UpdatedAt) {
		return c.f, nil
	}
	var f forge.Forge
	switch {
	case it.Kind == models.ForgeGitea:
		tok, err := s.box.Decrypt(it.TokenEnc)
		if err != nil {
			return nil, err
		}
		f = forge.NewGitea(it.BaseURL, it.Username, tok)
	case it.Kind == models.ForgeGitHub && it.AuthType == models.AuthGitHubApp:
		key, err := s.box.Decrypt(it.PrivateKeyEnc)
		if err != nil {
			return nil, err
		}
		if f, err = forge.NewGitHubApp(it.BaseURL, it.WebURL, it.AppID, it.InstallationID, []byte(key)); err != nil {
			return nil, err
		}
	case it.Kind == models.ForgeGitHub:
		tok, err := s.box.Decrypt(it.TokenEnc)
		if err != nil {
			return nil, err
		}
		f = forge.NewGitHubToken(it.BaseURL, it.WebURL, tok)
	default:
		return nil, fmt.Errorf("unsupported forge %q", it.Kind)
	}
	s.forges[it.ID] = cachedForge{updated: it.UpdatedAt, f: f}
	return f, nil
}

func (s *Service) integration(ctx context.Context, org, id string) (*models.Integration, error) {
	var it models.Integration
	if err := s.db.WithContext(ctx).First(&it, "id = ? AND organization_id = ?", id, org).Error; err != nil {
		return nil, ErrNotFound
	}
	return &it, nil
}

// TestIntegration checks an integration's credentials.
func (s *Service) TestIntegration(ctx context.Context, org, id string) (string, error) {
	it, err := s.integration(ctx, org, id)
	if err != nil {
		return "", err
	}
	f, err := s.Forge(it)
	if err != nil {
		return "", err
	}
	return f.Verify(ctx)
}

// ---- projects ----

// ProjectInput creates or updates a project.
type ProjectInput struct {
	Name          string
	Description   string
	IntegrationID string
	Owner         string
	Repo          string
	AgentID       *string
	Selector      []string
	SandboxImage  string
	Instructions  string
	TriggerLabel  string
	// CreateRepo creates the repository on the forge instead of connecting an existing one.
	CreateRepo bool
	Private    bool
}

var slugRE = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.Trim(slugRE.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if s == "" {
		s = "project"
	}
	if len(s) > 60 {
		s = s[:60]
	}
	return s
}

var repoNameRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)

// CreateProject connects (or creates) a repository and registers it as a project.
func (s *Service) CreateProject(ctx context.Context, org, userID string, in ProjectInput) (*models.Project, error) {
	if !repoNameRE.MatchString(in.Owner) || !repoNameRE.MatchString(in.Repo) {
		return nil, errors.New("owner and repo must be plain names (letters, digits, . _ -)")
	}
	it, err := s.integration(ctx, org, in.IntegrationID)
	if err != nil {
		return nil, errors.New("unknown integration")
	}
	if it.Kind != models.ForgeGitea && it.Kind != models.ForgeGitHub {
		return nil, errors.New("projects need a git forge integration (Gitea or GitHub)")
	}
	f, err := s.Forge(it)
	if err != nil {
		return nil, err
	}
	var repo *forge.Repo
	if in.CreateRepo {
		repo, err = f.CreateRepo(ctx, in.Owner, in.Repo, in.Description, in.Private)
	} else {
		repo, err = f.GetRepo(ctx, in.Owner, in.Repo)
	}
	if errors.Is(err, forge.ErrNotFound) {
		return nil, fmt.Errorf("repository %s/%s not found (or the credentials cannot see it)", in.Owner, in.Repo)
	}
	if err != nil {
		return nil, fmt.Errorf("forge: %w", err)
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = repo.Name
	}
	p := &models.Project{Base: models.Base{ID: models.NewID("prj"), OrganizationID: org}, Name: name, Slug: s.uniqueSlug(ctx, org, slugify(name)),
		Description: in.Description, IntegrationID: it.ID, Forge: it.Kind, Owner: repo.Owner, Repo: repo.Name, DefaultBranch: repo.DefaultBranch,
		WebURL: repo.WebURL, AgentID: emptyToNil(in.AgentID), Selector: nonNil(in.Selector), SandboxImage: strings.TrimSpace(in.SandboxImage),
		Instructions: in.Instructions, TriggerLabel: strings.TrimSpace(in.TriggerLabel), CreatedBy: userID}
	if p.DefaultBranch == "" {
		p.DefaultBranch = "main"
	}
	if err := s.db.WithContext(ctx).Create(p).Error; err != nil {
		return nil, err
	}
	s.audit.Best(ctx, audit.Entry{OrganizationID: org, ActorType: audit.ActorUser, ActorID: userID, Action: "project.create", TargetType: "project",
		TargetID: p.ID, Metadata: map[string]any{"repo": p.FullName(), "created_repo": in.CreateRepo, "integration_id": it.ID}})
	return p, nil
}

// UpdateProject edits a project's settings (the repository itself is fixed).
func (s *Service) UpdateProject(ctx context.Context, org, userID, id string, in ProjectInput) (*models.Project, error) {
	p, err := s.Project(ctx, org, id)
	if err != nil {
		return nil, err
	}
	if n := strings.TrimSpace(in.Name); n != "" {
		p.Name = n
	}
	p.Description, p.AgentID, p.Selector = in.Description, emptyToNil(in.AgentID), nonNil(in.Selector)
	p.SandboxImage, p.Instructions, p.TriggerLabel = strings.TrimSpace(in.SandboxImage), in.Instructions, strings.TrimSpace(in.TriggerLabel)
	if err := s.db.WithContext(ctx).Save(p).Error; err != nil {
		return nil, err
	}
	s.audit.Best(ctx, audit.Entry{OrganizationID: org, ActorType: audit.ActorUser, ActorID: userID, Action: "project.update", TargetType: "project", TargetID: p.ID})
	return p, nil
}

// Project loads a project.
func (s *Service) Project(ctx context.Context, org, id string) (*models.Project, error) {
	var p models.Project
	if err := s.db.WithContext(ctx).First(&p, "id = ? AND organization_id = ?", id, org).Error; err != nil {
		return nil, ErrNotFound
	}
	return &p, nil
}

func (s *Service) projectForge(ctx context.Context, org, projectID string) (*models.Project, forge.Forge, error) {
	p, err := s.Project(ctx, org, projectID)
	if err != nil {
		return nil, nil, err
	}
	it, err := s.integration(ctx, org, p.IntegrationID)
	if err != nil {
		return nil, nil, errors.New("the project's integration no longer exists")
	}
	f, err := s.Forge(it)
	return p, f, err
}

func (s *Service) uniqueSlug(ctx context.Context, org, base string) string {
	slug := base
	for i := 2; ; i++ {
		var n int64
		s.db.WithContext(ctx).Model(&models.Project{}).Where("organization_id = ? AND slug = ?", org, slug).Count(&n)
		if n == 0 {
			return slug
		}
		slug = fmt.Sprintf("%s-%d", base, i)
	}
}

// BranchFor names the working branch of a task or chat session. A task keeps its branch across
// attempts, so a retry continues the same pull request.
func BranchFor(taskID, sessionID string) string {
	if taskID != "" {
		return "akili/" + taskID
	}
	return "akili/" + sessionID
}

// Spec builds the session.open project section.
func (s *Service) Spec(ctx context.Context, sess *models.ChatSession, agent *models.Agent) (*proto.ProjectSpec, *models.Project, error) {
	if sess.ProjectID == nil {
		return nil, nil, nil
	}
	p, err := s.Project(ctx, sess.OrganizationID, *sess.ProjectID)
	if err != nil {
		return nil, nil, err
	}
	id := s.git.For(agent)
	trailer := "Akili-Session: " + sess.ID
	if sess.TaskID != nil {
		trailer = "Akili-Task: " + *sess.TaskID
	}
	return &proto.ProjectSpec{ID: p.ID, Slug: p.Slug, Name: p.Name, Repo: p.FullName(), DefaultBranch: p.DefaultBranch, Branch: sess.Branch,
		SandboxImage: p.SandboxImage, GitName: id.Name, GitEmail: id.Email, Trailers: []string{trailer}}, p, nil
}

func emptyToNil(s *string) *string {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	return s
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
