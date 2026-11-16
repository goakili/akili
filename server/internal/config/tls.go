// Copyright (c) 2026 Jonas Kaninda.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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
