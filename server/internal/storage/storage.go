// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package storage connects to Postgres and Redis, runs migrations and seeds first-run data.
package storage

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/config"
	"github.com/goakili/akili/server/internal/crypto"
	"github.com/goakili/akili/server/internal/models"
	"github.com/jkaninda/logger"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// ConnectPostgres opens the database, retrying while it starts (docker-compose ordering).
func ConnectPostgres(ctx context.Context, dsn string) (*gorm.DB, error) {
	var lastErr error
	for i := 0; i < 30; i++ {
		db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
		if err == nil {
			sqlDB, _ := db.DB()
			if err = sqlDB.PingContext(ctx); err == nil {
				sqlDB.SetMaxOpenConns(25)
				sqlDB.SetMaxIdleConns(5)
				logger.Info("database connected")
				return db, nil
			}
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return nil, fmt.Errorf("connect database: %w", lastErr)
}

// ConnectRedis opens and verifies a Redis client.
func ConnectRedis(ctx context.Context, opts *redis.Options) (*redis.Client, error) {
	client := redis.NewClient(opts)
	var err error
	for i := 0; i < 30; i++ {
		if err = client.Ping(ctx).Err(); err == nil {
			logger.Info("redis connected")
			return client, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return nil, fmt.Errorf("connect redis: %w", err)
}

// Seeded is what first-run seeding produced.
type Seeded struct {
	OrgID      string
	SigningKey ed25519.PrivateKey
}

// SigningKeySetting is the settings key of the encrypted policy-signing key.
const SigningKeySetting = "policy_signing_key"

// Seed ensures the organization, the first owner, built-in policies, a default model provider and the
// policy-signing key exist. It is idempotent.
func Seed(db *gorm.DB, cfg *config.Config, box *crypto.Box) (*Seeded, error) {
	var org models.Organization
	if err := db.First(&org).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		org = models.Organization{ID: models.NewID("org"), Name: "Default", Slug: "default"}
		if err := db.Create(&org).Error; err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	var users int64
	db.Model(&models.User{}).Count(&users)
	if users == 0 {
		password := cfg.AdminPassword
		generated := password == ""
		if generated {
			password = rand.Text()
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		u := models.User{Base: models.Base{ID: models.NewID("usr"), OrganizationID: org.ID}, Email: cfg.AdminEmail,
			Name: "Administrator", PasswordHash: string(hash), Role: models.RoleOwner, Active: true}
		if err := db.Create(&u).Error; err != nil {
			return nil, err
		}
		if generated {
			// Printed once so the operator can log in; it is not stored anywhere in plaintext.
			logger.Warn("created the first owner account; change this password after logging in",
				"email", cfg.AdminEmail, "password", password)
		} else {
			logger.Info("created the first owner account", "email", cfg.AdminEmail)
		}
	}

	// Built-in templates are read-only in the UI, so keep them in step with the shipped versions.
	for _, tpl := range proto.PolicyTemplates() {
		var p models.Policy
		err := db.Where("organization_id = ? AND name = ? AND builtin", org.ID, tpl.Name).First(&p).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			p = models.Policy{Base: models.Base{ID: models.NewID("pol"), OrganizationID: org.ID}, Name: tpl.Name,
				Description: "Built-in template", Version: tpl.Version, Document: tpl, Builtin: true}
			if err := db.Create(&p).Error; err != nil {
				return nil, err
			}
		case err != nil:
			return nil, err
		case p.Document.Version < tpl.Version:
			if err := db.Model(&p).Select("document", "version").Updates(&models.Policy{Document: tpl, Version: tpl.Version}).Error; err != nil {
				return nil, err
			}
			logger.Info("updated built-in policy template", "name", tpl.Name, "version", tpl.Version)
		}
	}

	if err := seedRunbooks(db, org.ID); err != nil {
		return nil, err
	}

	var providers int64
	db.Model(&models.ModelProvider{}).Count(&providers)
	if providers == 0 {
		p := models.ModelProvider{Base: models.Base{ID: models.NewID("prv"), OrganizationID: org.ID}, IsDefault: true, MaxTokens: 32000}
		if cfg.AnthropicAPIKey != "" {
			enc, err := box.Encrypt(cfg.AnthropicAPIKey)
			if err != nil {
				return nil, err
			}
			p.Name, p.Kind, p.Model, p.Effort, p.APIKeyEnc = "Anthropic", models.ProviderAnthropic, cfg.DefaultModel, "high", enc
		} else {
			// Without a key, start with the scripted provider so the platform is usable end to end.
			p.Name, p.Kind, p.Model = "Scripted (development)", models.ProviderFake, "fake"
			logger.Warn("ANTHROPIC_API_KEY is not set: seeded the scripted development provider; add a real provider in Settings")
		}
		if err := db.Create(&p).Error; err != nil {
			return nil, err
		}
	}

	key, err := signingKey(db, box)
	if err != nil {
		return nil, err
	}
	return &Seeded{OrgID: org.ID, SigningKey: key}, nil
}

func signingKey(db *gorm.DB, box *crypto.Box) (ed25519.PrivateKey, error) {
	var s models.Setting
	err := db.First(&s, "key = ?", SigningKeySetting).Error
	if err == nil {
		raw, err := box.Decrypt(s.Value)
		if err != nil {
			return nil, fmt.Errorf("policy signing key: %w (was AKILI_ENCRYPTION_KEY changed?)", err)
		}
		b, err := base64.StdEncoding.DecodeString(raw)
		if err != nil || len(b) != ed25519.PrivateKeySize {
			return nil, errors.New("policy signing key is corrupt")
		}
		return b, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	key, err := crypto.NewSigningKey()
	if err != nil {
		return nil, err
	}
	enc, err := box.Encrypt(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		return nil, err
	}
	if err := db.Create(&models.Setting{Key: SigningKeySetting, Value: enc}).Error; err != nil {
		return nil, err
	}
	return key, nil
}

//go:embed runbooks/*.md
var runbookFS embed.FS

// seedRunbooks installs the built-in runbook skills and keeps them in step with the shipped text.
func seedRunbooks(db *gorm.DB, org string) error {
	entries, err := runbookFS.ReadDir("runbooks")
	if err != nil {
		return err
	}
	for _, e := range entries {
		raw, err := runbookFS.ReadFile("runbooks/" + e.Name())
		if err != nil {
			return err
		}
		name, desc, body := parseFrontMatter(string(raw))
		hash := crypto.SHA256Hex([]byte(body))
		var sk models.Skill
		err = db.Where("organization_id = ? AND builtin AND name = ?", org, name).First(&sk).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			sk = models.Skill{Base: models.Base{ID: models.NewID("skl"), OrganizationID: org}, Builtin: true, Name: name, Description: desc,
				Content: body, Version: 1, Hash: hash}
			if err := db.Create(&sk).Error; err != nil {
				return err
			}
		case err != nil:
			return err
		case sk.Hash != hash:
			if err := db.Model(&sk).Updates(map[string]any{"content": body, "description": desc, "hash": hash, "version": sk.Version + 1}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func parseFrontMatter(s string) (name, desc, body string) {
	body = s
	if !strings.HasPrefix(s, "---\n") {
		return "", "", s
	}
	end := strings.Index(s[4:], "\n---\n")
	if end < 0 {
		return "", "", s
	}
	for _, line := range strings.Split(s[4:4+end], "\n") {
		k, v, _ := strings.Cut(line, ":")
		switch strings.TrimSpace(k) {
		case "name":
			name = strings.TrimSpace(v)
		case "description":
			desc = strings.TrimSpace(v)
		}
	}
	return name, desc, strings.TrimSpace(s[4+end+5:])
}
