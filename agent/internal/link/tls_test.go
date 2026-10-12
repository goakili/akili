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
