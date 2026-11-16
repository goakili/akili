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
