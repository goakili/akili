// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package link

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestTLSHint(t *testing.T) {
	err := fmt.Errorf("dial: %w", x509.UnknownAuthorityError{})
	if h := TLSHint(err); !strings.Contains(h.Error(), "AKILI_CA_CERT") || !errors.Is(h, err) {
		t.Fatalf("no hint: %v", h)
	}
	mac := fmt.Errorf("post: %w", &tls.CertificateVerificationError{Err: errors.New("x509: certificate is not trusted")})
	if h := TLSHint(mac); !strings.Contains(h.Error(), "AKILI_CA_CERT") {
		t.Fatalf("no hint for the macOS form: %v", h)
	}
	wrongHost := fmt.Errorf("post: %w", &tls.CertificateVerificationError{Err: x509.HostnameError{Host: "x"}})
	if h := TLSHint(wrongHost); strings.Contains(h.Error(), "AKILI_CA_CERT") {
		t.Fatalf("CA hint for a hostname mismatch: %v", h)
	}
	plain := errors.New("connection refused")
	if TLSHint(plain) != plain {
		t.Fatal("hint added to an unrelated error")
	}
}
