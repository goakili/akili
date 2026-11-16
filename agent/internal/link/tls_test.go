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
