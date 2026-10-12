// Copyright (c) 2026 Jonas Kaninda.
//
// This file is part of Akili.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

// ServerTLS builds the listener's TLS config. The certificate is reloaded when its file changes, so
// a renewed certificate (cert-manager, ACME) is picked up without a restart. With agent mTLS, any
// presented client certificate must chain to the agent CA; whether one is required is decided per
// endpoint, because browsers on the same listener have none.
func (t TLSConfig) ServerTLS() (*tls.Config, error) {
	r := &certReloader{certFile: t.CertFile, keyFile: t.KeyFile}
	if err := r.load(); err != nil {
		return nil, err
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: r.get}
	if t.AgentMTLS == "optional" || t.AgentMTLS == "required" {
		pem, err := os.ReadFile(t.AgentClientCA)
		if err != nil {
			return nil, fmt.Errorf("agent client CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("agent client CA file contains no certificates")
		}
		cfg.ClientCAs = pool
		cfg.ClientAuth = tls.VerifyClientCertIfGiven
	}
	return cfg, nil
}

type certReloader struct {
	certFile, keyFile string
	mu                sync.RWMutex
	cert              *tls.Certificate
	mod               time.Time
	checked           time.Time
}

func (r *certReloader) load() error {
	st, err := os.Stat(r.certFile)
	if err != nil {
		return fmt.Errorf("TLS certificate: %w", err)
	}
	c, err := tls.LoadX509KeyPair(r.certFile, r.keyFile)
	if err != nil {
		return fmt.Errorf("TLS certificate: %w", err)
	}
	r.mu.Lock()
	r.cert, r.mod, r.checked = &c, st.ModTime(), time.Now()
	r.mu.Unlock()
	return nil
}

func (r *certReloader) get(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	r.mu.RLock()
	cert, mod, checked := r.cert, r.mod, r.checked
	r.mu.RUnlock()
	if time.Since(checked) > 30*time.Second {
		if st, err := os.Stat(r.certFile); err == nil && st.ModTime().After(mod) {
			if err := r.load(); err == nil {
				r.mu.RLock()
				cert = r.cert
				r.mu.RUnlock()
			}
		} else {
			r.mu.Lock()
			r.checked = time.Now()
			r.mu.Unlock()
		}
	}
	return cert, nil
}
