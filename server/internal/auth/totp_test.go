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

package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/goakili/akili/server/internal/crypto"
)

// RFC 6238 appendix B (SHA-1), truncated to 6 digits.
func TestTOTPCodeRFC6238(t *testing.T) {
	key := []byte("12345678901234567890")
	for unix, want := range map[int64]string{59: "287082", 1111111109: "081804", 1111111111: "050471", 1234567890: "005924", 2000000000: "279037"} {
		if got := totpCode(key, unix/totpPeriod); got != want {
			t.Errorf("t=%d: got %s, want %s", unix, got, want)
		}
	}
}

func TestMatchTOTPSkew(t *testing.T) {
	key := []byte("12345678901234567890")
	now := time.Unix(1111111109, 0)
	cur := now.Unix() / totpPeriod
	for d := int64(-1); d <= 1; d++ {
		step, ok := matchTOTP(key, totpCode(key, cur+d), now)
		if !ok || step != cur+d {
			t.Errorf("offset %d: ok=%v step=%d", d, ok, step)
		}
	}
	for _, d := range []int64{-2, 2} {
		if _, ok := matchTOTP(key, totpCode(key, cur+d), now); ok {
			t.Errorf("offset %d accepted", d)
		}
	}
	if _, ok := matchTOTP(key, "", now); ok {
		t.Error("empty code accepted")
	}
}

func TestCodeShapes(t *testing.T) {
	if normalizeCode(" 123 456 ") != "123456" || !isTOTPCode(normalizeCode("123-456")) {
		t.Error("TOTP code not normalised")
	}
	if isTOTPCode("12345a") || isTOTPCode("1234567") {
		t.Error("non-TOTP shape accepted")
	}
	plain, hashes := newRecoveryCodes()
	if len(plain) != RecoveryCodeCount || len(hashes) != RecoveryCodeCount {
		t.Fatalf("got %d codes", len(plain))
	}
	seen := map[string]bool{}
	for i, c := range plain {
		if len(c) != 11 || c[5] != '-' || isTOTPCode(normalizeCode(c)) {
			t.Errorf("bad recovery code %q", c)
		}
		if crypto.HashToken(normalizeCode(strings.ToUpper(c))) != hashes[i] {
			t.Errorf("recovery code %d does not match its hash", i)
		}
		if seen[c] {
			t.Errorf("duplicate recovery code %q", c)
		}
		seen[c] = true
	}
}

func TestOtpauthURIAndQR(t *testing.T) {
	uri := otpauthURI("a+b@example.com", "JBSWY3DPEHPK3PXP")
	if !strings.HasPrefix(uri, "otpauth://totp/Akili:a+b@example.com?") || !strings.Contains(uri, "secret=JBSWY3DPEHPK3PXP") || !strings.Contains(uri, "issuer=Akili") {
		t.Errorf("unexpected URI %s", uri)
	}
	img, err := qrDataURL(uri)
	if err != nil || !strings.HasPrefix(img, "data:image/png;base64,") {
		t.Fatalf("qr: %v %.40s", err, img)
	}
}
