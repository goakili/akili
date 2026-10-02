// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package siem

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// WebhookSink POSTs batches as JSON ({"events":[...]}) or NDJSON (one event per line, e.g. for
// Splunk HEC or Vector). With a secret, the body is signed: X-Akili-Signature: sha256=<hmac hex>.
type WebhookSink struct {
	URL           string
	Secret        string
	Authorization string // sent as the Authorization header, e.g. "Splunk <token>"
	NDJSON        bool
	Client        *http.Client
}

// Name implements Sink.
func (w *WebhookSink) Name() string { return "webhook" }

// Send implements Sink.
func (w *WebhookSink) Send(ctx context.Context, events []Event) error {
	var body []byte
	ctype := "application/json"
	if w.NDJSON {
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		for i := range events {
			if err := enc.Encode(events[i]); err != nil {
				return err
			}
		}
		body, ctype = b.Bytes(), "application/x-ndjson"
	} else {
		var err error
		if body, err = json.Marshal(map[string]any{"events": events}); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", ctype)
	req.Header.Set("User-Agent", "akili-siem")
	if w.Secret != "" {
		m := hmac.New(sha256.New, []byte(w.Secret))
		m.Write(body)
		req.Header.Set("X-Akili-Signature", "sha256="+hex.EncodeToString(m.Sum(nil)))
	}
	if w.Authorization != "" {
		req.Header.Set("Authorization", w.Authorization)
	}
	c := w.Client
	if c == nil {
		c = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned %d", resp.StatusCode)
	}
	return nil
}

// SyslogSink sends RFC 5424 messages whose MSG is the event JSON. TCP and TLS use octet-counting
// framing (RFC 6587/5425); UDP sends one datagram per event.
type SyslogSink struct {
	Network  string // udp | tcp | tls
	Addr     string
	Hostname string
	TLS      *tls.Config

	mu   sync.Mutex
	conn net.Conn
}

// NewSyslogSink parses udp://host:port, tcp://host:port or tls://host:port.
func NewSyslogSink(raw string) (*SyslogSink, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("syslog address must look like udp://host:514, tcp://host:514 or tls://host:6514")
	}
	switch u.Scheme {
	case "udp", "tcp", "tls":
	default:
		return nil, fmt.Errorf("syslog scheme must be udp, tcp or tls, not %q", u.Scheme)
	}
	host, _ := os.Hostname()
	return &SyslogSink{Network: u.Scheme, Addr: u.Host, Hostname: host, TLS: &tls.Config{MinVersion: tls.VersionTLS12}}, nil
}

// Name implements Sink.
func (s *SyslogSink) Name() string { return "syslog" }

func (s *SyslogSink) dial(ctx context.Context) (net.Conn, error) {
	d := &net.Dialer{Timeout: 10 * time.Second}
	switch s.Network {
	case "tls":
		cfg := s.TLS.Clone()
		if cfg.ServerName == "" {
			cfg.ServerName, _, _ = net.SplitHostPort(s.Addr)
		}
		return (&tls.Dialer{NetDialer: d, Config: cfg}).DialContext(ctx, "tcp", s.Addr)
	default:
		return d.DialContext(ctx, s.Network, s.Addr)
	}
}

// Send implements Sink. A failed write drops the connection so the next attempt redials.
func (s *SyslogSink) Send(ctx context.Context, events []Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil {
		c, err := s.dial(ctx)
		if err != nil {
			return err
		}
		s.conn = c
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = s.conn.SetWriteDeadline(dl)
	}
	for i := range events {
		msg, err := s.format(&events[i])
		if err != nil {
			return err
		}
		if s.Network != "udp" {
			msg = append([]byte(fmt.Sprintf("%d ", len(msg))), msg...)
		}
		if _, err := s.conn.Write(msg); err != nil {
			_ = s.conn.Close()
			s.conn = nil
			return err
		}
	}
	return nil
}

// format builds <PRI>1 TIMESTAMP HOST APP PROCID MSGID [SD] MSG. Facility is authpriv (10);
// severity is notice (5) for denials and failures, informational (6) otherwise.
func (s *SyslogSink) format(e *Event) ([]byte, error) {
	body, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	sev := 6
	if strings.Contains(e.Action, "deny") || strings.Contains(e.Action, "denied") || strings.Contains(e.Action, "fail") || strings.Contains(e.Action, "refused") {
		sev = 5
	}
	host := s.Hostname
	if host == "" {
		host = "-"
	}
	msgID := e.Action
	if len(msgID) > 32 {
		msgID = msgID[:32]
	}
	return []byte(fmt.Sprintf("<%d>1 %s %s akili - %s - %s", 10*8+sev, e.CreatedAt.UTC().Format(time.RFC3339Nano), host, msgID, body)), nil
}

// FileSink appends JSON lines to a file, or writes them to stdout when the path is "-" (for a log
// shipper such as Fluent Bit or Vector).
type FileSink struct {
	Path string
	mu   sync.Mutex
}

// Name implements Sink.
func (f *FileSink) Name() string { return "file" }

// Send implements Sink.
func (f *FileSink) Send(_ context.Context, events []Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	for i := range events {
		if err := enc.Encode(events[i]); err != nil {
			return err
		}
	}
	if f.Path == "-" {
		_, err := os.Stdout.Write(b.Bytes())
		return err
	}
	file, err := os.OpenFile(f.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(b.Bytes()); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
