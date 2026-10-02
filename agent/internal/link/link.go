// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package link keeps the agent connected to the control plane and dispatches the streams the
// control plane opens: one control stream per tunnel, one stream per session.
package link

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/goakili/akili/agent/internal/runtime"
	"github.com/goakili/akili/agent/internal/state"
	"github.com/goakili/akili/proto"
	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"
	"github.com/jkaninda/logger"
	"github.com/jkaninda/wstunnel"
)

// Agent is the connected agent process.
type Agent struct {
	st      *state.State
	key     ed25519.PrivateKey
	version string
	facts   func() proto.HostFacts
	// ShellEnv is passed to shell tools (see AKILI_AGENT_SHELL_ENV).
	ShellEnv []string

	mu       sync.Mutex
	current  *yamux.Session
	git      *gitProxy
	sessions map[string]*runtime.Session
	draining bool
	wg       sync.WaitGroup
}

// New returns an agent.
func New(st *state.State, version string, facts func() proto.HostFacts) (*Agent, error) {
	key, err := st.Key()
	if err != nil {
		return nil, err
	}
	if len(st.CPSigningKey) != ed25519.PublicKeySize {
		return nil, errors.New("state has no pinned control-plane signing key; re-enroll")
	}
	a := &Agent{st: st, key: key, version: version, facts: facts, sessions: map[string]*runtime.Session{}}
	if err := a.startGitProxy(); err != nil {
		return nil, err
	}
	return a, nil
}

// tunnel returns the live tunnel, if connected.
func (a *Agent) tunnel() *yamux.Session {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.current
}

// Run connects and reconnects until ctx ends, then waits up to drainTimeout for sessions to finish.
func (a *Agent) Run(ctx context.Context, drainTimeout time.Duration) error {
	dialer, err := Dialer(a.st.CACert, a.st.Insecure)
	if err != nil {
		return err
	}
	url := wstunnel.URL(a.st.URL, proto.ConnectPath)
	backoff := time.Second
	for ctx.Err() == nil {
		sess, err := a.dial(ctx, dialer, url)
		if errors.Is(err, ErrUnauthorized) {
			logger.Error("the control plane refused this agent's identity (it was revoked, re-enrolled, deleted, or the control plane uses a different database); " +
				"the reason is in the control plane's audit log (action agent.connect_refused). To recover, click Re-enroll on the agent page and run `akili-agent enroll --force` with the new token")
			backoff = 30 * time.Second
		}
		if err == nil {
			logger.Info("connected to control plane", "url", a.st.URL, "agent", a.st.AgentID)
			backoff = time.Second
			err = a.serve(ctx, sess)
			_ = sess.Close()
		}
		if ctx.Err() != nil {
			break
		}
		logger.Warn("control plane connection lost; retrying", "error", TLSHint(err), "in", backoff)
		select {
		case <-ctx.Done():
		case <-time.After(backoff + jitter(backoff)):
		}
		if backoff *= 2; backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
	return a.drain(drainTimeout)
}

// ErrUnauthorized means the control plane rejected the agent's signed handshake.
var ErrUnauthorized = errors.New("control plane rejected the agent identity (401)")

// dial opens the tunnel. It dials the WebSocket itself (rather than wstunnel.Dial) to see the HTTP
// status of a refused handshake, and signs a fresh handshake per attempt.
func (a *Agent) dial(ctx context.Context, dialer *websocket.Dialer, url string) (*yamux.Session, error) {
	ws, resp, err := dialer.DialContext(ctx, url, a.handshake())
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusUnauthorized {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
			if bytes.Contains(body, []byte("client certificate")) {
				return nil, errors.New("the control plane requires an agent client certificate; set AKILI_CLIENT_CERT_FILE and AKILI_CLIENT_KEY_FILE")
			}
			return nil, ErrUnauthorized
		}
		return nil, err
	}
	sess, err := wstunnel.Server(ws) // the control plane opens streams; this side accepts them
	if err != nil {
		_ = ws.Close()
		return nil, err
	}
	return sess, nil
}

// handshake signs a fresh timestamp and nonce for each connection attempt.
func (a *Agent) handshake() http.Header {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := rand.Text()
	sig := ed25519.Sign(a.key, proto.HandshakeMessage(a.st.AgentID, ts, nonce))
	h := http.Header{}
	h.Set(proto.HeaderAgentID, a.st.AgentID)
	h.Set(proto.HeaderTimestamp, ts)
	h.Set(proto.HeaderNonce, nonce)
	h.Set(proto.HeaderSignature, base64.StdEncoding.EncodeToString(sig))
	h.Set(proto.HeaderVersion, a.version)
	h.Set(proto.HeaderFeatures, "sessions,tasks,llm-gateway")
	return h
}

// serve accepts the streams the control plane opens on one tunnel.
func (a *Agent) serve(ctx context.Context, sess *yamux.Session) error {
	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	a.mu.Lock()
	a.current = sess
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		if a.current == sess {
			a.current = nil
		}
		a.mu.Unlock()
	}()
	go func() {
		<-connCtx.Done()
		_ = sess.Close()
	}()
	llm := runtime.NewLLM(func() (net.Conn, error) { return sess.Open() })
	for {
		stream, err := sess.AcceptStream()
		if err != nil {
			return err
		}
		go a.handleStream(connCtx, proto.NewConn(stream), llm)
	}
}

func (a *Agent) handleStream(ctx context.Context, conn *proto.Conn, llm runtime.Completer) {
	env, err := conn.Recv()
	if err != nil {
		_ = conn.Close()
		return
	}
	switch env.Type {
	case proto.TypeHello:
		a.control(ctx, conn, env)
	case proto.TypeSessionOpen:
		a.session(ctx, conn, env, llm)
	case proto.TypePTYOpen:
		a.terminal(ctx, conn, env)
	default:
		logger.Warn("unknown stream kind", "type", env.Type)
		_ = conn.Close()
	}
}

func (a *Agent) control(ctx context.Context, conn *proto.Conn, hello proto.Envelope) {
	defer conn.Close()
	var h proto.Hello
	_ = hello.Decode(&h)
	a.mu.Lock()
	a.draining = h.Draining
	a.mu.Unlock()
	logger.Info("registered with control plane", "name", h.Name, "labels", h.Labels, "draining", h.Draining)
	go func() {
		for {
			env, err := conn.Recv()
			if err != nil {
				return
			}
			if env.Type == proto.TypeDrain {
				var body struct {
					Draining bool `json:"draining"`
				}
				_ = env.Decode(&body)
				a.mu.Lock()
				a.draining = body.Draining
				a.mu.Unlock()
				logger.Info("drain state changed", "draining", body.Draining)
			}
		}
	}()
	t := time.NewTicker(proto.HeartbeatInterval)
	defer t.Stop()
	for {
		if err := conn.Send(proto.TypeHeartbeat, a.heartbeat()); err != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (a *Agent) heartbeat() proto.Heartbeat {
	a.mu.Lock()
	defer a.mu.Unlock()
	hb := proto.Heartbeat{Facts: a.facts(), Draining: a.draining, ActiveSessions: []string{}}
	for id := range a.sessions {
		hb.ActiveSessions = append(hb.ActiveSessions, id)
	}
	return hb
}

func (a *Agent) session(ctx context.Context, conn *proto.Conn, env proto.Envelope, llm runtime.Completer) {
	defer conn.Close()
	open, err := runtime.DecodeOpen(env)
	if err != nil {
		_ = conn.Send(proto.TypeError, proto.Error{Message: err.Error()})
		return
	}
	s, err := runtime.NewSession(runtime.Config{Workdir: a.st.Workdir, CPSigningKey: a.st.CPSigningKey, Facts: a.facts, ShellEnv: a.ShellEnv, Protected: []string{a.st.Dir}, GitRemote: a.git.Remote}, conn, llm, open)
	if err != nil {
		logger.Error("refusing session", "session", open.SessionID, "error", err)
		_ = conn.Send(proto.TypeError, proto.Error{Message: err.Error()})
		return
	}
	a.mu.Lock()
	if old := a.sessions[s.ID()]; old != nil {
		logger.Warn("session reopened while still running; the newer stream wins", "session", s.ID())
	}
	a.sessions[s.ID()] = s
	a.wg.Add(1)
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		if a.sessions[s.ID()] == s {
			delete(a.sessions, s.ID())
		}
		a.mu.Unlock()
		a.wg.Done()
	}()
	logger.Info("session started", "session", open.SessionID, "mode", open.Mode, "task", open.TaskID)
	s.Run(ctx)
	logger.Info("session ended", "session", open.SessionID)
}

// drain waits for running sessions to end (their contexts are already cancelled).
func (a *Agent) drain(timeout time.Duration) error {
	done := make(chan struct{})
	go func() { a.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("sessions still running after %s", timeout)
	}
}

// TLSHint explains an untrusted control-plane certificate, the usual first error with a self-signed
// control plane. A wrong hostname or an expired certificate needs another fix, so it gets no hint.
func TLSHint(err error) error {
	var unknown x509.UnknownAuthorityError
	var verify *tls.CertificateVerificationError
	var host x509.HostnameError
	var invalid x509.CertificateInvalidError
	if errors.As(err, &host) || errors.As(err, &invalid) {
		return err
	}
	// macOS's verifier reports an unknown CA as "certificate is not trusted" rather than as an
	// UnknownAuthorityError.
	if errors.As(err, &unknown) || errors.As(err, &verify) {
		return fmt.Errorf("%w (the control plane's certificate is not signed by a trusted CA: set AKILI_CA_CERT to its CA file, or AKILI_CA_CERT_PEM to the PEM)", err)
	}
	return err
}

// Dialer builds the WebSocket dialer. A CA bundle is added to the system pool rather than replacing
// it; insecure is a loudly-logged development escape hatch.
func Dialer(caFile string, insecure bool) (*websocket.Dialer, error) {
	d := *websocket.DefaultDialer
	d.HandshakeTimeout = 15 * time.Second
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read CA: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("CA file contains no certificates")
		}
		cfg.RootCAs = pool
	}
	if insecure {
		logger.Warn("TLS verification is DISABLED (--insecure); never use this in production")
		cfg.InsecureSkipVerify = true //nolint:gosec // explicit opt-in
	}
	if certFile, keyFile := os.Getenv("AKILI_CLIENT_CERT_FILE"), os.Getenv("AKILI_CLIENT_KEY_FILE"); certFile != "" || keyFile != "" {
		if certFile == "" || keyFile == "" {
			return nil, errors.New("set both AKILI_CLIENT_CERT_FILE and AKILI_CLIENT_KEY_FILE")
		}
		if _, err := tls.LoadX509KeyPair(certFile, keyFile); err != nil {
			return nil, fmt.Errorf("client certificate: %w", err)
		}
		// Read per handshake so a renewed certificate is used without a restart.
		cfg.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			c, err := tls.LoadX509KeyPair(certFile, keyFile)
			return &c, err
		}
	}
	d.TLSClientConfig = cfg
	return &d, nil
}

// HTTPClient returns an HTTP client with the same TLS settings, for enrollment.
func HTTPClient(caFile string, insecure bool) (*http.Client, error) {
	d, err := Dialer(caFile, insecure)
	if err != nil {
		return nil, err
	}
	return &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{TLSClientConfig: d.TLSClientConfig}}, nil
}

func jitter(d time.Duration) time.Duration {
	b := make([]byte, 1)
	_, _ = rand.Read(b)
	return time.Duration(int64(d) * int64(b[0]) / 1024)
}
