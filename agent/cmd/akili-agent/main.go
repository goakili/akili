// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command akili-agent is the Akili agent: it enrolls with a control plane, keeps an outbound tunnel
// to it, and runs the sessions and tasks it is given, within the policy it is given.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/goakili/akili/agent/internal/host"
	"github.com/goakili/akili/agent/internal/link"
	"github.com/goakili/akili/agent/internal/procsec"
	"github.com/goakili/akili/agent/internal/state"
	"github.com/goakili/akili/proto"
	"github.com/jkaninda/logger"
)

// Version is set at build time.
var Version = "dev"

const defaultStateDir = "/var/lib/akili-agent"

func main() {
	// Before anything reads the key or session credentials: tools run as this user.
	if err := procsec.NoDump(); err != nil {
		logger.Warn("cannot mark the process non-dumpable; same-user processes may read its memory", "error", err)
	}
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "enroll":
		err = enroll(os.Args[2:])
	case "run":
		err = run(os.Args[2:])
	case "version":
		fmt.Println(Version)
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "akili-agent:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `Usage: akili-agent <command> [flags]

Commands:
  enroll   Enroll with a control plane using a one-time join token (AKILI_JOIN_TOKEN)
  run      Connect to the control plane and serve sessions
  version  Print the version

Environment: AKILI_URL, AKILI_JOIN_TOKEN, AKILI_STATE_DIR, AKILI_AGENT_WORKDIR, AKILI_INSECURE,
  AKILI_CA_CERT, AKILI_CA_CERT_PEM
                          CA to trust for a control plane with a self-signed or private-CA
                          certificate: a PEM file path, or the PEM itself (or base64 of it); copied into the state
                          directory and used by enroll and run
  AKILI_AGENT_SHELL_ENV   comma-separated variable names passed to shell tools (e.g. KUBECONFIG,PATH);
                          everything else is scrubbed from the tools' environment
  AKILI_CLIENT_CERT_FILE, AKILI_CLIENT_KEY_FILE
                          client certificate for control planes that require agent mTLS
`)
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func setupLogger() {
	opts := []logger.Option{logger.WithLevel(logger.LogLevel(env("AKILI_LOG_LEVEL", "info")))}
	if env("AKILI_LOG_FORMAT", "json") == "json" {
		opts = append(opts, logger.WithJSONFormat())
	}
	logger.New(opts...)
}

func enroll(args []string) error {
	fs := flag.NewFlagSet("enroll", flag.ExitOnError)
	url := fs.String("url", env("AKILI_URL", ""), "control plane URL")
	stateDir := fs.String("state-dir", env("AKILI_STATE_DIR", defaultStateDir), "state directory")
	workdir := fs.String("workdir", env("AKILI_AGENT_WORKDIR", ""), "working directory for tools (default <state-dir>/work)")
	caCert := fs.String("ca-cert", env("AKILI_CA_CERT", ""), "CA bundle (PEM file) to trust for the control plane, e.g. its self-signed CA")
	insecure := fs.Bool("insecure", env("AKILI_INSECURE", "") == "true", "skip TLS verification (development only)")
	force := fs.Bool("force", false, "re-enroll even if already enrolled")
	_ = fs.Parse(args)
	setupLogger()

	// The token comes from the environment only, never a flag, so it does not appear in `ps`.
	token := os.Getenv("AKILI_JOIN_TOKEN")
	if *url == "" || token == "" {
		return errors.New("AKILI_URL (or --url) and AKILI_JOIN_TOKEN are required")
	}
	if _, err := state.Load(*stateDir); err == nil && !*force {
		return fmt.Errorf("already enrolled (state in %s); use --force to re-enroll", *stateDir)
	}
	if *workdir == "" {
		*workdir = filepath.Join(*stateDir, "work")
	}
	abs, err := filepath.Abs(*workdir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return err
	}
	// Policies match real paths, so the workdir itself must be symlink-free.
	if abs, err = filepath.EvalSymlinks(abs); err != nil {
		return err
	}
	ca, err := state.InstallCA(*stateDir, *caCert, os.Getenv("AKILI_CA_CERT_PEM"))
	if err != nil {
		return err
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	body, _ := json.Marshal(proto.EnrollRequest{JoinToken: token, PublicKey: pub, Facts: host.Facts(Version, abs)})
	client, err := link.HTTPClient(ca, *insecure)
	if err != nil {
		return err
	}
	resp, err := client.Post(strings.TrimRight(*url, "/")+proto.EnrollPath, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("enroll: %w", link.TLSHint(err))
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode == http.StatusUnauthorized && bytes.Contains(raw, []byte("client certificate")) {
		return errors.New("enroll rejected: the control plane requires an agent client certificate; set AKILI_CLIENT_CERT_FILE and AKILI_CLIENT_KEY_FILE")
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("enroll rejected: the join token is invalid, already used or expired. Join tokens are single-use: " +
			"if this machine was enrolled before, keep its state and just run `akili-agent run`; otherwise click Re-enroll on the agent page for a new token")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("enroll rejected: %s: %s", resp.Status, bytes.TrimSpace(raw))
	}
	var er proto.EnrollResponse
	if err := json.Unmarshal(raw, &er); err != nil {
		return fmt.Errorf("enroll: bad response: %w", err)
	}
	st := &state.State{URL: strings.TrimRight(*url, "/"), AgentID: er.AgentID, Name: er.Name, PrivateKey: priv,
		CPSigningKey: er.CPSigningKey, Workdir: abs, CACert: ca, Insecure: *insecure}
	if err := state.Save(*stateDir, st); err != nil {
		return err
	}
	logger.Info("enrolled", "agent", er.AgentID, "name", er.Name, "state", *stateDir, "workdir", abs)
	return nil
}

func run(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	stateDir := fs.String("state-dir", env("AKILI_STATE_DIR", defaultStateDir), "state directory")
	caCert := fs.String("ca-cert", env("AKILI_CA_CERT", ""), "CA bundle (PEM file) to trust for the control plane; replaces the one saved at enrollment")
	_ = fs.Parse(args)
	setupLogger()

	// Docker-style bootstrap: enroll on first start when a join token is provided.
	if _, err := state.Load(*stateDir); err != nil && os.Getenv("AKILI_JOIN_TOKEN") != "" {
		if err := enroll([]string{"--state-dir", *stateDir}); err != nil {
			return err
		}
	}
	st, err := state.Load(*stateDir)
	if err != nil {
		return err
	}
	// A CA given at run time (a renewed self-signed CA, or one missing at enrollment) replaces the
	// saved one, without re-enrolling.
	if *caCert != "" || os.Getenv("AKILI_CA_CERT_PEM") != "" {
		ca, err := state.InstallCA(*stateDir, *caCert, os.Getenv("AKILI_CA_CERT_PEM"))
		if err != nil {
			return err
		}
		if st.CACert != ca {
			st.CACert = ca
			if err := state.Save(*stateDir, st); err != nil {
				return err
			}
		}
		logger.Info("trusting a custom CA for the control plane", "ca", ca)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	a, err := link.New(st, Version, func() proto.HostFacts { return host.Facts(Version, st.Workdir) })
	if err != nil {
		return err
	}
	a.ShellEnv = shellEnv(os.Getenv("AKILI_AGENT_SHELL_ENV"))
	logger.Info("akili agent starting", "version", Version, "agent", st.AgentID, "control_plane", st.URL, "workdir", st.Workdir)
	err = a.Run(ctx, 50*time.Second)
	logger.Info("akili agent stopped")
	return err
}

// shellEnv resolves the variable names an operator chose to pass to shell tools. Values are read
// once at start; the names are logged, never the values.
func shellEnv(names string) []string {
	var out, passed []string
	for _, n := range strings.Split(names, ",") {
		n = strings.TrimSpace(n)
		if n == "" || strings.HasPrefix(n, "AKILI_") {
			continue // the agent's own settings (join token, URL) never reach tools
		}
		if v, ok := os.LookupEnv(n); ok {
			out = append(out, n+"="+v)
			passed = append(passed, n)
		}
	}
	if len(passed) > 0 {
		logger.Info("passing environment to shell tools", "variables", strings.Join(passed, ","))
	}
	return out
}
