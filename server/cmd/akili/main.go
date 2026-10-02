// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command akili is the Akili control plane.
package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/audit"
	"github.com/goakili/akili/server/internal/auth"
	"github.com/goakili/akili/server/internal/bus"
	"github.com/goakili/akili/server/internal/chat"
	"github.com/goakili/akili/server/internal/coder"
	"github.com/goakili/akili/server/internal/config"
	"github.com/goakili/akili/server/internal/dto"
	"github.com/goakili/akili/server/internal/fleet"
	"github.com/goakili/akili/server/internal/gateway"
	"github.com/goakili/akili/server/internal/handlers"
	"github.com/goakili/akili/server/internal/leader"
	"github.com/goakili/akili/server/internal/lessons"
	"github.com/goakili/akili/server/internal/mcp"
	"github.com/goakili/akili/server/internal/miabi"
	"github.com/goakili/akili/server/internal/middlewares"
	"github.com/goakili/akili/server/internal/notify"
	"github.com/goakili/akili/server/internal/procsec"
	"github.com/goakili/akili/server/internal/routes"
	"github.com/goakili/akili/server/internal/sessions"
	"github.com/goakili/akili/server/internal/siem"
	"github.com/goakili/akili/server/internal/storage"
	"github.com/goakili/akili/server/internal/storage/migration"
	"github.com/goakili/akili/server/internal/tasks"
	"github.com/jkaninda/logger"
	"github.com/jkaninda/okapi"
	"github.com/jkaninda/okapi/okapicli"
)

func main() {
	// Before anything reads secrets: stdio MCP servers run as this user.
	if err := procsec.NoDump(); err != nil {
		logger.Warn("cannot mark the process non-dumpable; same-user processes may read its memory", "error", err)
	}
	app := okapi.New()
	cli := okapicli.New(app, "Akili")

	cli.Command("server", "Start the Akili control plane", func(cmd *okapicli.Command) error {
		return runServer(cli)
	})
	cli.Command("migrate", "Run database migrations and exit", func(cmd *okapicli.Command) error {
		cfg := config.Load()
		db, err := storage.ConnectPostgres(context.Background(), cfg.DatabaseURL)
		if err != nil {
			return err
		}
		return migration.Run(db)
	})
	cli.Command("keys", "Manage encryption keys: keys status | keys rotate | keys rewrap", func(cmd *okapicli.Command) error {
		return runKeys(cmd.Args())
	})
	cli.Command("version", "Print the version", func(cmd *okapicli.Command) error {
		fmt.Println(config.Version)
		return nil
	})
	cli.DefaultCommand("server")
	if err := cli.Execute(); err != nil {
		logger.Fatal(err.Error())
	}
}

func runServer(cli *okapicli.CLI) error {
	cfg := config.Load()
	app := cli.Okapi()
	if err := cfg.Initialize(app, dto.ErrorHandler()); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db, err := storage.ConnectPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	if err := migration.Run(db); err != nil {
		return err
	}
	redisOpts, err := cfg.RedisOptions()
	if err != nil {
		return err
	}
	rdb, err := storage.ConnectRedis(ctx, redisOpts)
	if err != nil {
		return err
	}
	box, err := openBox(ctx, cfg, db)
	if err != nil {
		return err
	}
	if cfg.TLS.Enabled() {
		tlsCfg, err := cfg.TLS.ServerTLS()
		if err != nil {
			return err
		}
		app.With(okapi.WithTLS(tlsCfg))
	}
	seeded, err := storage.Seed(db, cfg, box)
	if err != nil {
		return fmt.Errorf("seed: %w", err)
	}

	b := bus.New(rdb)
	auditLog := audit.New(db)
	notifier := notify.New(cfg.NotifyWebhookURL, cfg.PublicURL)
	mailer := notify.NewMailer(db, box, auditLog, rdb, cfg.PublicURL)
	notifier.SetMailer(mailer)
	hub := sessions.New(db, b, auditLog, notifier, seeded.SigningKey)
	gw := gateway.New(db, box, auditLog, hub)
	coderSvc := coder.New(db, box, auditLog, cfg.Git)
	hub.SetRemoteTools(coderSvc)
	miabiSvc := miabi.NewService(db, box, auditLog)
	hub.AddRemoteRunner(miabi.Tools, miabiSvc.RunRemote)
	lessonSvc := lessons.New(db, auditLog)
	hub.AddRemoteRunner([]string{proto.ToolLessonPropose}, lessonSvc.RunRemote)
	hub.SetLessons(lessonSvc.ForPrompt)
	mcpSvc := mcp.New(db, box, auditLog, cfg.MCPCommands, cfg.MCPBinDir)
	hub.SetMCP(mcpSvc.Tools, mcpSvc.RunRemote)
	gw.Mount(func(agentID string, mux *http.ServeMux) {
		mux.Handle(proto.GitProxyPath, coderSvc.GitProxy(agentID))
	})
	fleetSvc := fleet.NewService(db, b, auditLog, hub.SigningPublicKey(), cfg.PublicURL, cfg.Git)
	tunnels := fleet.NewManager(db, b, auditLog, hub, gw)
	elector := leader.New(rdb, "control-plane")
	taskSvc := tasks.New(db, b, auditLog, hub, notifier, elector)
	authSvc := auth.New(db, rdb, cfg.JWTSecret, cfg.SessionTTL)
	var sso *auth.OIDC
	if cfg.OIDC.Enabled() {
		sso = auth.NewOIDC(cfg.OIDC, cfg.PublicURL, db, rdb, authSvc)
		authSvc.PasswordOwnerOnly = cfg.OIDC.DisablePassword
	}
	sinks, err := siemSinks(cfg.SIEM)
	if err != nil {
		return err
	}
	forwarder := siem.New(db, elector, cfg.SIEM.FromStart, sinks...)
	chatSvc := chat.New(db, box, b, auditLog, hub, taskSvc, elector)

	h := &handlers.Handlers{Cfg: cfg, DB: db, Bus: b, Audit: auditLog, Auth: authSvc, Fleet: fleetSvc, Tunnels: tunnels,
		Sessions: hub, Tasks: taskSvc, Box: box, Coder: coderSvc, Miabi: miabiSvc, OIDC: sso, SIEM: forwarder, Chat: chatSvc, Lessons: lessonSvc, MCP: mcpSvc, Mail: mailer}
	routes.Register(app, h, middlewares.NewAuthenticator(db, authSvc))

	return cli.RunServer(&okapicli.RunOptions{
		ShutdownTimeout: 30 * time.Second,
		OnStart: func() {
			go elector.Run(ctx)
			go taskSvc.Run(ctx)
			go forwarder.Run(ctx)
			go chatSvc.Run(ctx)
			go mcpSvc.RunRefresh(ctx)
			go miabiSvc.RunEvents(ctx, elector, func(ctx context.Context, t *miabi.Trigger) { h.MiabiTrigger(ctx, t) })
			logger.Info("akili control plane starting", "version", config.Version, "port", cfg.Port, "public_url", cfg.PublicURL,
				"kms", box.Keyring().Provider(), "tls", cfg.TLS.Enabled(), "agent_mtls", cfg.TLS.AgentMTLS, "sso", sso != nil, "siem_sinks", len(sinks))
		},
		OnShutdown: func() {
			cancel()
			tunnels.Shutdown()
		},
	})
}
