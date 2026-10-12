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

package state

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CAFileName is where the trusted control-plane CA is kept, next to the state.
const CAFileName = "ca.pem"

// InstallCA validates a CA bundle, given as a file path or inline PEM (inline wins), and copies it
// into the state directory, so the agent keeps trusting it after the source is gone (a temporary
// mount, a deleted download). It returns the installed path, or "" when neither is given.
func InstallCA(dir, file, inline string) (string, error) {
	var data []byte
	switch {
	case inline != "":
		data = []byte(inline)
		// Single-line inputs (a marketplace form, some env editors) cannot hold PEM line breaks,
		// so base64-encoded PEM is accepted too.
		if !strings.Contains(inline, "-----BEGIN") {
			b, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(inline), ""))
			if err != nil {
				return "", errors.New("AKILI_CA_CERT_PEM must be PEM or base64-encoded PEM")
			}
			data = b
		}
	case file != "":
		b, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("read CA %s: %w", file, err)
		}
		data = b
	default:
		return "", nil
	}
	if err := checkPEM(data); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, CAFileName)
	if abs, err := filepath.Abs(file); err == nil && inline == "" && abs == dst {
		return dst, nil
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return "", err
	}
	return dst, os.Rename(tmp, dst)
}

// checkPEM requires at least one parseable certificate, so a wrong file fails at setup rather than
// as a TLS error on every connection attempt.
func checkPEM(data []byte) error {
	n := 0
	for {
		var b *pem.Block
		b, data = pem.Decode(data)
		if b == nil {
			break
		}
		if b.Type != "CERTIFICATE" {
			continue
		}
		if _, err := x509.ParseCertificate(b.Bytes); err != nil {
			return fmt.Errorf("CA bundle has an invalid certificate: %w", err)
		}
		n++
	}
	if n == 0 {
		return errors.New("CA bundle contains no PEM certificate")
	}
	return nil
}
