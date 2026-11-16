// Copyright (c) 2026 Jonas Kaninda.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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

// ServerTLS reloads the certificate on file change; agent client certs are required per endpoint, since browsers share the listener.
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
