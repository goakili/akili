// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package config loads the control plane's configuration from the environment and configures Okapi.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/goakili/akili/server/internal/gitid"
	goutils "github.com/jkaninda/go-utils"
	"github.com/jkaninda/logger"
	"github.com/jkaninda/okapi"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

// Version is set at build time with -ldflags.
var Version = "dev"

const minSecretLen = 32

// devEncryptionKey is used only in development when AKILI_ENCRYPTION_KEY is unset. validate()
// refuses it in production.
const devEncryptionKey = "akili-development-encryption-key-do-not-use"

// Config is the control plane configuration.
type Config struct {
	Env         string
	Port        int
	PublicURL   string
	LogLevel    string
	DatabaseURL string
	// License is an Akili Enterprise license token, installed on start when none is stored.
	License string

	// RedisURL (redis:// or rediss:// for TLS) overrides RedisAddr, RedisPassword and RedisDB.
	RedisURL      string
	RedisAddr     string
	RedisPassword string
	RedisDB       int

	JWTSecret     string
	SessionTTL    time.Duration
	CookieSecure  bool
	EncryptionKey string
	CORSOrigins   []string

	AdminEmail    string
	AdminPassword string

	// AnthropicAPIKey seeds the default model provider on first start.
	AnthropicAPIKey string
	DefaultModel    string

	NotifyWebhookURL string
	WebDir           string
	// AgentDownloadsDir holds the akili-agent-linux-{amd64,arm64} binaries served at /downloads
	// (the image ships them); install-agent.sh downloads from there.
	AgentDownloadsDir string
	OpenAPIDocs       bool
	// TrustedProxies are the CIDRs whose X-Forwarded-For / X-Real-IP headers are believed. Empty
	// means none: the client IP (rate limits, audit) is always the connection address.
	TrustedProxies []string
	// MCPCommands are the executables an admin may run as stdio MCP servers (by base name).
	MCPCommands []string
	// MCPBinDir is searched before PATH for those executables; it must be read-only to the server.
	MCPBinDir string
	// Git is the commit identity of agents (AKILI_GIT_EMAIL_TEMPLATE, AKILI_GIT_EMAIL_DOMAINS).
	Git gitid.Config

	KMS   string // local | vault-transit: who holds the key that wraps the data keys
	Vault VaultConfig
	SIEM  SIEMConfig
	OIDC  OIDCConfig
	TLS   TLSConfig
}

// VaultConfig is the Vault (or OpenBao) Transit key that wraps the data keys.
type VaultConfig struct {
	Addr      string
	Token     string
	Mount     string
	Key       string
	Namespace string
}

// SIEMConfig names the audit sinks. Any combination may be set.
type SIEMConfig struct {
	WebhookURL    string
	WebhookSecret string
	WebhookAuth   string // Authorization header value
	WebhookNDJSON bool
	Syslog        string // udp://, tcp:// or tls://host:port
	File          string // path, or "-" for stdout
	FromStart     bool   // a new sink first receives the whole existing trail
}

// OIDCConfig enables single sign-on with an OpenID Connect provider.
type OIDCConfig struct {
	Issuer          string
	ClientID        string
	ClientSecret    string
	Name            string // login button label
	Scopes          []string
	AllowedDomains  []string // email domains allowed to sign in; empty allows any verified email
	DefaultRole     string   // role for new SSO users
	RoleClaim       string   // claim holding group names, e.g. "groups"
	RoleMap         map[string]string
	DisablePassword bool // password login only for the owner (break-glass)
}

// Enabled reports whether SSO is configured.
func (o OIDCConfig) Enabled() bool { return o.Issuer != "" && o.ClientID != "" }

// TLSConfig serves HTTPS directly and, optionally, requires agents to present a client certificate.
type TLSConfig struct {
	CertFile      string
	KeyFile       string
	AgentClientCA string // PEM bundle that signs agent client certificates
	AgentMTLS     string // off | optional | required
}

// Enabled reports whether the server terminates TLS itself.
func (t TLSConfig) Enabled() bool { return t.CertFile != "" && t.KeyFile != "" }

// Load reads the environment, after loading an optional .env file.
func Load() *Config {
	loadEnvFile()
	c := &Config{
		Env:               goutils.Env("AKILI_ENV", "development"),
		Port:              goutils.EnvInt("AKILI_PORT", 8080),
		PublicURL:         strings.TrimRight(goutils.Env("AKILI_PUBLIC_URL", "http://localhost:8080"), "/"),
		LogLevel:          goutils.Env("AKILI_LOG_LEVEL", "info"),
		License:           envOrFile("AKILI_LICENSE"),
		DatabaseURL:       goutils.Env("AKILI_DATABASE_URL", "postgres://akili:akili@localhost:5432/akili?sslmode=disable"),
		RedisURL:          envOrFile("AKILI_REDIS_URL"),
		RedisAddr:         goutils.Env("AKILI_REDIS_ADDR", "localhost:6379"),
		RedisPassword:     goutils.Env("AKILI_REDIS_PASSWORD", ""),
		RedisDB:           goutils.EnvInt("AKILI_REDIS_DB", 0),
		JWTSecret:         goutils.Env("AKILI_JWT_SECRET", ""),
		CookieSecure:      goutils.EnvBool("AKILI_COOKIE_SECURE", false),
		EncryptionKey:     goutils.Env("AKILI_ENCRYPTION_KEY", ""),
		AdminEmail:        goutils.Env("AKILI_ADMIN_EMAIL", "admin@akili.local"),
		AdminPassword:     goutils.Env("AKILI_ADMIN_PASSWORD", ""),
		AnthropicAPIKey:   goutils.Env("ANTHROPIC_API_KEY", ""),
		DefaultModel:      goutils.Env("AKILI_DEFAULT_MODEL", "claude-opus-5-5"),
		NotifyWebhookURL:  goutils.Env("AKILI_NOTIFY_WEBHOOK_URL", ""),
		WebDir:            goutils.Env("AKILI_WEB_DIR", ""),
		AgentDownloadsDir: goutils.Env("AKILI_AGENT_DOWNLOADS_DIR", ""),
		OpenAPIDocs:       goutils.EnvBool("AKILI_OPENAPI_DOCS", true),
		TrustedProxies:    list(goutils.Env("AKILI_TRUSTED_PROXIES", "")),
		MCPCommands:       list(goutils.Env("AKILI_MCP_COMMANDS", "miabi")),
		MCPBinDir:         goutils.Env("AKILI_MCP_BIN_DIR", ""),
		Git: gitid.Config{
			EmailTemplate:  goutils.Env("AKILI_GIT_EMAIL_TEMPLATE", gitid.DefaultEmailTemplate),
			AllowedDomains: list(strings.ToLower(goutils.Env("AKILI_GIT_EMAIL_DOMAINS", ""))),
		},
		KMS: goutils.Env("AKILI_KMS", "local"),
		Vault: VaultConfig{
			Addr:      goutils.Env("AKILI_VAULT_ADDR", ""),
			Token:     envOrFile("AKILI_VAULT_TOKEN"),
			Mount:     goutils.Env("AKILI_VAULT_TRANSIT_MOUNT", "transit"),
			Key:       goutils.Env("AKILI_VAULT_TRANSIT_KEY", "akili"),
			Namespace: goutils.Env("AKILI_VAULT_NAMESPACE", ""),
		},
		SIEM: SIEMConfig{
			WebhookURL:    goutils.Env("AKILI_SIEM_WEBHOOK_URL", ""),
			WebhookSecret: envOrFile("AKILI_SIEM_WEBHOOK_SECRET"),
			WebhookAuth:   envOrFile("AKILI_SIEM_WEBHOOK_AUTHORIZATION"),
			WebhookNDJSON: goutils.Env("AKILI_SIEM_WEBHOOK_FORMAT", "json") == "ndjson",
			Syslog:        goutils.Env("AKILI_SIEM_SYSLOG", ""),
			File:          goutils.Env("AKILI_SIEM_FILE", ""),
			FromStart:     goutils.EnvBool("AKILI_SIEM_FROM_START", true),
		},
		OIDC: OIDCConfig{
			Issuer:          strings.TrimRight(goutils.Env("AKILI_OIDC_ISSUER", ""), "/"),
			ClientID:        goutils.Env("AKILI_OIDC_CLIENT_ID", ""),
			ClientSecret:    envOrFile("AKILI_OIDC_CLIENT_SECRET"),
			Name:            goutils.Env("AKILI_OIDC_NAME", "SSO"),
			Scopes:          list(goutils.Env("AKILI_OIDC_SCOPES", "openid,email,profile")),
			AllowedDomains:  list(strings.ToLower(goutils.Env("AKILI_OIDC_ALLOWED_DOMAINS", ""))),
			DefaultRole:     goutils.Env("AKILI_OIDC_DEFAULT_ROLE", "viewer"),
			RoleClaim:       goutils.Env("AKILI_OIDC_ROLE_CLAIM", "groups"),
			RoleMap:         pairs(goutils.Env("AKILI_OIDC_ROLE_MAP", "")),
			DisablePassword: goutils.EnvBool("AKILI_OIDC_DISABLE_PASSWORD", false),
		},
		TLS: TLSConfig{
			CertFile:      goutils.Env("AKILI_TLS_CERT_FILE", ""),
			KeyFile:       goutils.Env("AKILI_TLS_KEY_FILE", ""),
			AgentClientCA: goutils.Env("AKILI_AGENT_CLIENT_CA_FILE", ""),
			AgentMTLS:     goutils.Env("AKILI_AGENT_MTLS", "off"),
		},
	}
	c.SessionTTL = time.Duration(goutils.EnvInt("AKILI_SESSION_TTL_HOURS", 12)) * time.Hour
	for _, o := range strings.Split(goutils.Env("AKILI_CORS_ORIGINS", ""), ",") {
		if o = strings.TrimSpace(o); o != "" {
			c.CORSOrigins = append(c.CORSOrigins, o)
		}
	}
	if c.IsDev() {
		if c.JWTSecret == "" {
			c.JWTSecret = "akili-development-jwt-secret-change-me-please"
		}
		if c.EncryptionKey == "" && c.KMS == "local" {
			c.EncryptionKey = devEncryptionKey
		}
	}
	return c
}

// loadEnvFile loads AKILI_ENV_FILE, or ./.env when unset, into the process environment. Variables
// already set in the real environment win, so a deployment's env always overrides the file. A missing
// ./.env is normal. A file that exists but cannot be parsed stops the server: silently falling back to
// defaults would point it at the wrong database.
func loadEnvFile() {
	path, explicit := os.Getenv("AKILI_ENV_FILE"), true
	if path == "" {
		path, explicit = ".env", false
	}
	if _, err := os.Stat(path); err != nil {
		if explicit || !errors.Is(err, fs.ErrNotExist) {
			logger.Fatal("cannot read environment file", "path", path, "error", err)
		}
		return
	}
	if err := godotenv.Load(path); err != nil {
		logger.Fatal("invalid environment file; each line must be KEY=value or a # comment", "path", path, "error", err)
	}
	logger.Info("loaded environment file", "path", path)
}

// envOrFile reads NAME, or the file named by NAME_FILE (Kubernetes/Docker secrets).
func envOrFile(name string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	if p := os.Getenv(name + "_FILE"); p != "" {
		b, err := os.ReadFile(p)
		if err != nil {
			logger.Fatal("cannot read secret file", "variable", name+"_FILE", "error", err)
		}
		return strings.TrimSpace(string(b))
	}
	return ""
}

func list(s string) []string {
	var out []string
	for _, v := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' }) {
		out = append(out, strings.TrimSpace(v))
	}
	return out
}

// pairs parses "a=b,c=d".
func pairs(s string) map[string]string {
	out := map[string]string{}
	for _, kv := range list(s) {
		if k, v, ok := strings.Cut(kv, "="); ok && k != "" && v != "" {
			out[k] = v
		}
	}
	return out
}

// RedisOptions returns the Redis connection settings: AKILI_REDIS_URL when set, otherwise
// AKILI_REDIS_ADDR, AKILI_REDIS_PASSWORD and AKILI_REDIS_DB.
func (c *Config) RedisOptions() (*redis.Options, error) {
	if c.RedisURL != "" {
		opts, err := redis.ParseURL(c.RedisURL)
		if err != nil {
			// The URL may hold a password: never echo it.
			return nil, errors.New("AKILI_REDIS_URL is invalid; use redis://[user:password@]host:port[/db] or rediss:// for TLS")
		}
		return opts, nil
	}
	return &redis.Options{Addr: c.RedisAddr, Password: c.RedisPassword, DB: c.RedisDB}, nil
}

// IsDev reports whether the control plane runs in development mode.
func (c *Config) IsDev() bool { return c.Env == "development" || c.Env == "dev" }

// Validate refuses unsafe production settings.
func (c *Config) Validate() error {
	var errs []error
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("AKILI_DATABASE_URL is required"))
	}
	if _, err := c.RedisOptions(); err != nil {
		errs = append(errs, err)
	}
	errs = append(errs, c.validateHardening()...)
	if c.IsDev() {
		return errors.Join(errs...)
	}
	if len(c.JWTSecret) < minSecretLen {
		errs = append(errs, fmt.Errorf("AKILI_JWT_SECRET must be at least %d characters", minSecretLen))
	}
	if c.KMS == "local" && (len(c.EncryptionKey) < minSecretLen || c.EncryptionKey == devEncryptionKey) {
		errs = append(errs, fmt.Errorf("AKILI_ENCRYPTION_KEY must be at least %d characters (openssl rand -hex 32)", minSecretLen))
	}
	if c.SIEM.WebhookURL != "" && !strings.HasPrefix(c.SIEM.WebhookURL, "https://") {
		errs = append(errs, errors.New("AKILI_SIEM_WEBHOOK_URL must be https in production"))
	}
	if strings.HasPrefix(c.SIEM.Syslog, "udp://") || strings.HasPrefix(c.SIEM.Syslog, "tcp://") {
		logger.Warn("AKILI_SIEM_SYSLOG is not encrypted; use tls:// unless the collector is local", "syslog", c.SIEM.Syslog)
	}
	if c.OIDC.Enabled() && !strings.HasPrefix(c.OIDC.Issuer, "https://") {
		errs = append(errs, errors.New("AKILI_OIDC_ISSUER must be https in production"))
	}
	if c.Vault.Addr != "" && !strings.HasPrefix(c.Vault.Addr, "https://") {
		errs = append(errs, errors.New("AKILI_VAULT_ADDR must be https in production"))
	}
	for _, o := range c.CORSOrigins {
		if o == "*" {
			errs = append(errs, errors.New("AKILI_CORS_ORIGINS must not contain * in production"))
		}
	}
	if !strings.HasPrefix(c.PublicURL, "https://") {
		logger.Warn("AKILI_PUBLIC_URL is not https; agents will connect without TLS", "url", c.PublicURL)
	}
	if !c.CookieSecure {
		logger.Warn("AKILI_COOKIE_SECURE is false; session cookies will be sent over plain HTTP")
	}
	return errors.Join(errs...)
}

// validateHardening checks the hardening options in every environment: they are all opt-in, so a
// half-configured option is a mistake, not a default.
func (c *Config) validateHardening() []error {
	var errs []error
	if err := c.Git.Validate(); err != nil {
		errs = append(errs, err)
	}
	if c.MCPBinDir != "" {
		if fi, err := os.Stat(c.MCPBinDir); !filepath.IsAbs(c.MCPBinDir) || err != nil || !fi.IsDir() {
			errs = append(errs, fmt.Errorf("AKILI_MCP_BIN_DIR: %q is not an absolute path to a directory", c.MCPBinDir))
		}
	}
	for _, p := range c.TrustedProxies {
		if _, _, err := net.ParseCIDR(p); err != nil {
			errs = append(errs, fmt.Errorf("AKILI_TRUSTED_PROXIES: %q is not a CIDR (e.g. 10.0.0.0/8)", p))
		}
	}
	switch c.KMS {
	case "local":
	case "vault-transit":
		if c.Vault.Addr == "" || c.Vault.Token == "" || c.Vault.Key == "" {
			errs = append(errs, errors.New("AKILI_KMS=vault-transit needs AKILI_VAULT_ADDR, AKILI_VAULT_TOKEN (or _FILE) and AKILI_VAULT_TRANSIT_KEY"))
		}
	default:
		errs = append(errs, fmt.Errorf("AKILI_KMS must be local or vault-transit, not %q", c.KMS))
	}
	if c.OIDC.Issuer != "" || c.OIDC.ClientID != "" {
		if !c.OIDC.Enabled() || c.OIDC.ClientSecret == "" {
			errs = append(errs, errors.New("SSO needs AKILI_OIDC_ISSUER, AKILI_OIDC_CLIENT_ID and AKILI_OIDC_CLIENT_SECRET"))
		}
		roles := map[string]bool{"viewer": true, "operator": true, "admin": true}
		if !roles[c.OIDC.DefaultRole] {
			errs = append(errs, fmt.Errorf("AKILI_OIDC_DEFAULT_ROLE must be viewer, operator or admin, not %q", c.OIDC.DefaultRole))
		}
		for g, r := range c.OIDC.RoleMap {
			if !roles[r] {
				errs = append(errs, fmt.Errorf("AKILI_OIDC_ROLE_MAP: group %q maps to %q; roles are viewer, operator, admin (owner is never granted by SSO)", g, r))
			}
		}
	}
	switch c.TLS.AgentMTLS {
	case "off":
	case "optional", "required":
		if !c.TLS.Enabled() || c.TLS.AgentClientCA == "" {
			errs = append(errs, errors.New("AKILI_AGENT_MTLS needs AKILI_TLS_CERT_FILE, AKILI_TLS_KEY_FILE and AKILI_AGENT_CLIENT_CA_FILE"))
		}
	default:
		errs = append(errs, fmt.Errorf("AKILI_AGENT_MTLS must be off, optional or required, not %q", c.TLS.AgentMTLS))
	}
	if (c.TLS.CertFile == "") != (c.TLS.KeyFile == "") {
		errs = append(errs, errors.New("set both AKILI_TLS_CERT_FILE and AKILI_TLS_KEY_FILE"))
	}
	return errs
}

// Initialize configures the Okapi application.
func (c *Config) Initialize(app *okapi.Okapi, errorHandler okapi.ErrorHandler) error {
	if err := c.Validate(); err != nil {
		return err
	}
	opts := []logger.Option{logger.WithLevel(logger.LogLevel(c.LogLevel))}
	if !c.IsDev() {
		opts = append(opts, logger.WithJSONFormat())
	}
	l := logger.New(opts...)
	app.WithLogger(l.Logger)
	app.WithPort(c.Port)
	if len(c.TrustedProxies) > 0 {
		app.With(okapi.WithTrustedProxies(c.TrustedProxies...))
	} else {
		// With no entries okapi trusts forwarded headers from every peer; 0.0.0.0/32 matches no real
		// peer, so the headers are ignored and nobody can choose their own IP.
		app.With(okapi.WithTrustedProxies("0.0.0.0/32"))
	}
	if c.IsDev() {
		logger.Warn("running in development mode: default secrets are in use; never expose this instance")
	}
	if len(c.CORSOrigins) > 0 {
		app.WithCORS(okapi.Cors{
			AllowedOrigins:   c.CORSOrigins,
			AllowedHeaders:   []string{"Content-Type", "Authorization", "X-Request-ID"},
			AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
			AllowCredentials: true,
		})
	}
	if c.OpenAPIDocs {
		app.WithOpenAPIDocs(okapi.OpenAPI{
			Title:       "Akili API",
			Version:     Version,
			Description: "Security-first control plane for autonomous AI operator agents.",
			License:     okapi.License{Name: "Apache-2.0"},
			SecuritySchemes: okapi.SecuritySchemes{
				{Name: "BearerAuth", Type: "http", Scheme: "bearer", BearerFormat: "JWT or ak_ API key"},
			},
			UI: okapi.ScalarUI,
		})
	} else {
		app.WithOpenAPIDisabled()
	}
	app.WithErrorHandler(errorHandler)
	return nil
}
