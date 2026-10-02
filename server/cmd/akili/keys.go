// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/goakili/akili/server/internal/config"
	"github.com/goakili/akili/server/internal/crypto"
	"github.com/goakili/akili/server/internal/models"
	"github.com/goakili/akili/server/internal/siem"
	"github.com/goakili/akili/server/internal/storage"
	"github.com/goakili/akili/server/internal/storage/migration"
	"gorm.io/gorm"
)

// openBox opens the keyring with the configured KEK. The local KEK is also offered for reading when
// AKILI_ENCRYPTION_KEY is set alongside a KMS, so keys wrapped before the move stay readable until
// `akili keys rewrap`.
func openBox(ctx context.Context, cfg *config.Config, db *gorm.DB) (*crypto.Box, error) {
	var local crypto.KEK
	if cfg.EncryptionKey != "" {
		k, err := crypto.NewLocalKEK(cfg.EncryptionKey)
		if err != nil {
			return nil, err
		}
		local = k
	}
	kek := local
	if cfg.KMS == "vault-transit" {
		kek = &crypto.VaultTransitKEK{Addr: cfg.Vault.Addr, Token: cfg.Vault.Token, Mount: cfg.Vault.Mount, Key: cfg.Vault.Key, Namespace: cfg.Vault.Namespace}
	}
	if kek == nil {
		return nil, errors.New("no encryption key: set AKILI_ENCRYPTION_KEY or AKILI_KMS=vault-transit")
	}
	ring, err := crypto.OpenKeyring(ctx, kek, storage.SettingsKeyStore{DB: db}, cfg.EncryptionKey, local)
	if err != nil {
		return nil, fmt.Errorf("encryption keys: %w", err)
	}
	return crypto.NewBox(ring), nil
}

func siemSinks(c config.SIEMConfig) ([]siem.Sink, error) {
	var sinks []siem.Sink
	if c.WebhookURL != "" {
		sinks = append(sinks, &siem.WebhookSink{URL: c.WebhookURL, Secret: c.WebhookSecret, Authorization: c.WebhookAuth, NDJSON: c.WebhookNDJSON})
	}
	if c.Syslog != "" {
		s, err := siem.NewSyslogSink(c.Syslog)
		if err != nil {
			return nil, err
		}
		sinks = append(sinks, s)
	}
	if c.File != "" {
		sinks = append(sinks, &siem.FileSink{Path: c.File})
	}
	return sinks, nil
}

// encryptedColumns lists every column holding a Box ciphertext, for re-encryption.
var encryptedColumns = []struct {
	model   any
	table   string
	columns []string
}{
	{&models.ModelProvider{}, "model_providers", []string{"api_key_enc"}},
	{&models.Integration{}, "integrations", []string{"token_enc", "private_key_enc", "webhook_secret_enc"}},
	{&models.ChatChannel{}, "chat_channels", []string{"token_enc", "secret_enc"}},
	{&models.MCPServer{}, "mcp_servers", []string{"env_enc"}},
}

func runKeys(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: akili keys status | rotate | rewrap")
	}
	ctx := context.Background()
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		return err
	}
	db, err := storage.ConnectPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	if err := migration.Run(db); err != nil {
		return err
	}
	box, err := openBox(ctx, cfg, db)
	if err != nil {
		return err
	}
	ring := box.Keyring()
	switch args[0] {
	case "status":
		keys, err := ring.Keys(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("KEK provider: %s\n", ring.Provider())
		for _, k := range keys {
			mark := ""
			if k.Active {
				mark = " (active)"
			}
			fmt.Printf("data key %s wrapped by %s%s\n", k.ID, k.Provider, mark)
		}
		counts, err := ciphertextsByKey(db)
		if err != nil {
			return err
		}
		for kid, n := range counts {
			fmt.Printf("secrets sealed with %s: %d\n", kid, n)
		}
		if counts["legacy"] > 0 {
			fmt.Println("legacy (v1) secrets remain: keep AKILI_ENCRYPTION_KEY until `akili keys rotate` re-encrypts them")
		}
		return nil
	case "rotate":
		kid, err := ring.Rotate(ctx)
		if err != nil {
			return err
		}
		n, err := reencryptAll(db, box)
		if err != nil {
			return fmt.Errorf("new data key %s is active, but re-encryption stopped: %w (run rotate again)", kid, err)
		}
		fmt.Printf("new active data key %s; re-encrypted %d secrets\n", kid, n)
		return nil
	case "rewrap":
		n, err := ring.Rewrap(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("rewrapped %d data keys with %s\n", n, ring.Provider())
		return nil
	}
	return fmt.Errorf("unknown keys command %q", args[0])
}

// reencryptAll seals every stored secret under the active data key, in one transaction.
func reencryptAll(db *gorm.DB, box *crypto.Box) (int, error) {
	n := 0
	err := db.Transaction(func(tx *gorm.DB) error {
		for _, t := range encryptedColumns {
			var rows []map[string]any
			if err := tx.Table(t.table).Select(append([]string{"id"}, t.columns...)).Find(&rows).Error; err != nil {
				return err
			}
			for _, r := range rows {
				updates := map[string]any{}
				for _, col := range t.columns {
					v, _ := r[col].(string)
					out, changed, err := box.Reencrypt(v)
					if err != nil {
						return fmt.Errorf("%s.%s of %v: %w", t.table, col, r["id"], err)
					}
					if changed {
						updates[col] = out
					}
				}
				if len(updates) > 0 {
					if err := tx.Table(t.table).Where("id = ?", r["id"]).Updates(updates).Error; err != nil {
						return err
					}
					n += len(updates)
				}
			}
		}
		var settings []models.Setting
		if err := tx.Where("key = ?", storage.SigningKeySetting).Find(&settings).Error; err != nil {
			return err
		}
		for _, s := range settings {
			out, changed, err := box.Reencrypt(s.Value)
			if err != nil {
				return fmt.Errorf("setting %s: %w", s.Key, err)
			}
			if changed {
				if err := tx.Model(&models.Setting{}).Where("key = ?", s.Key).Update("value", out).Error; err != nil {
					return err
				}
				n++
			}
		}
		return nil
	})
	return n, err
}

func ciphertextsByKey(db *gorm.DB) (map[string]int, error) {
	out := map[string]int{}
	for _, t := range encryptedColumns {
		var rows []map[string]any
		if err := db.Table(t.table).Select(t.columns).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			for _, col := range t.columns {
				if v, _ := r[col].(string); v != "" {
					out[crypto.KeyID(v)]++
				}
			}
		}
	}
	var s models.Setting
	if db.First(&s, "key = ?", storage.SigningKeySetting).Error == nil && strings.TrimSpace(s.Value) != "" {
		out[crypto.KeyID(s.Value)]++
	}
	return out, nil
}
