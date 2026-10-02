// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadReadsEnvFileWithoutOverridingRealEnv(t *testing.T) {
	file := filepath.Join(t.TempDir(), "test.env")
	if err := os.WriteFile(file, []byte("AKILI_PORT=9999\nAKILI_LOG_LEVEL=error\n# comment\nAKILI_PUBLIC_URL=https://akili.example.com/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AKILI_ENV_FILE", file)
	t.Setenv("AKILI_LOG_LEVEL", "debug") // the real environment wins over the file
	for _, k := range []string{"AKILI_PORT", "AKILI_PUBLIC_URL"} {
		old, had := os.LookupEnv(k)
		os.Unsetenv(k)
		t.Cleanup(func() {
			if had {
				os.Setenv(k, old)
			} else {
				os.Unsetenv(k)
			}
		})
	}
	c := Load()
	if c.Port != 9999 {
		t.Errorf("port = %d, want 9999 from the env file", c.Port)
	}
	if c.LogLevel != "debug" {
		t.Errorf("log level = %q, want the real environment's value", c.LogLevel)
	}
	if c.PublicURL != "https://akili.example.com" {
		t.Errorf("public url = %q", c.PublicURL)
	}
}

func TestLoadWithoutEnvFile(t *testing.T) {
	t.Setenv("AKILI_ENV_FILE", "")
	t.Chdir(t.TempDir()) // no ./.env here
	if c := Load(); c.Port == 0 {
		t.Fatal("defaults not applied")
	}
}

func prodConfig() *Config {
	return &Config{Env: "production", DatabaseURL: "postgres://x", PublicURL: "https://a", CookieSecure: true, KMS: "local",
		JWTSecret: "0123456789abcdef0123456789abcdef", EncryptionKey: "0123456789abcdef0123456789abcdef",
		OIDC: OIDCConfig{DefaultRole: "viewer"}, TLS: TLSConfig{AgentMTLS: "off"}}
}

func TestValidateHardening(t *testing.T) {
	cases := []struct {
		name  string
		edit  func(*Config)
		valid bool
	}{
		{"baseline", func(*Config) {}, true},
		{"vault without token", func(c *Config) { c.KMS = "vault-transit"; c.Vault = VaultConfig{Addr: "https://v", Key: "k"} }, false},
		{"vault needs no local key", func(c *Config) {
			c.KMS, c.EncryptionKey = "vault-transit", ""
			c.Vault = VaultConfig{Addr: "https://v", Token: "t", Key: "k"}
		}, true},
		{"vault over http", func(c *Config) {
			c.KMS = "vault-transit"
			c.Vault = VaultConfig{Addr: "http://v", Token: "t", Key: "k"}
		}, false},
		{"unknown kms", func(c *Config) { c.KMS = "aws" }, false},
		{"sso without secret", func(c *Config) { c.OIDC.Issuer, c.OIDC.ClientID = "https://idp", "akili" }, false},
		{"sso ok", func(c *Config) { c.OIDC.Issuer, c.OIDC.ClientID, c.OIDC.ClientSecret = "https://idp", "akili", "s" }, true},
		{"sso never grants owner", func(c *Config) {
			c.OIDC.Issuer, c.OIDC.ClientID, c.OIDC.ClientSecret = "https://idp", "akili", "s"
			c.OIDC.RoleMap = map[string]string{"root": "owner"}
		}, false},
		{"sso over http", func(c *Config) { c.OIDC.Issuer, c.OIDC.ClientID, c.OIDC.ClientSecret = "http://idp", "akili", "s" }, false},
		{"mtls without tls", func(c *Config) { c.TLS.AgentMTLS = "required" }, false},
		{"mtls ok", func(c *Config) {
			c.TLS = TLSConfig{CertFile: "c", KeyFile: "k", AgentClientCA: "ca", AgentMTLS: "required"}
		}, true},
		{"half a certificate", func(c *Config) { c.TLS.CertFile = "c" }, false},
		{"siem webhook over http", func(c *Config) { c.SIEM.WebhookURL = "http://siem" }, false},
	}
	for _, tc := range cases {
		c := prodConfig()
		tc.edit(c)
		if err := c.Validate(); (err == nil) != tc.valid {
			t.Errorf("%s: valid=%v, err=%v", tc.name, tc.valid, err)
		}
	}
}

func TestPairsAndList(t *testing.T) {
	m := pairs("admins=admin, ops=operator,bad,=x")
	if len(m) != 2 || m["admins"] != "admin" || m["ops"] != "operator" {
		t.Fatalf("pairs = %v", m)
	}
	if l := list("openid, email profile"); len(l) != 3 {
		t.Fatalf("list = %v", l)
	}
}

func TestRedisOptions(t *testing.T) {
	c := &Config{RedisAddr: "localhost:6379", RedisPassword: "p", RedisDB: 2}
	if o, err := c.RedisOptions(); err != nil || o.Addr != "localhost:6379" || o.Password != "p" || o.DB != 2 {
		t.Fatalf("addr form: %+v %v", o, err)
	}
	c.RedisURL = "rediss://akili:s3cret@redis.example.com:6380/3"
	o, err := c.RedisOptions()
	if err != nil || o.Addr != "redis.example.com:6380" || o.Username != "akili" || o.Password != "s3cret" || o.DB != 3 || o.TLSConfig == nil {
		t.Fatalf("url form: %+v %v", o, err)
	}
	c.RedisURL = "http://akili:leaked-password@x"
	if _, err := c.RedisOptions(); err == nil || strings.Contains(err.Error(), "leaked-password") {
		t.Fatalf("bad URL: %v", err)
	}
	if err := (&Config{Env: "development", DatabaseURL: "x", KMS: "local", RedisURL: "nope://", TLS: TLSConfig{AgentMTLS: "off"}, OIDC: OIDCConfig{DefaultRole: "viewer"}}).Validate(); err == nil {
		t.Fatal("an invalid AKILI_REDIS_URL passed validation")
	}
}
