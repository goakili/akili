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

// InstallCA copies the CA into the state dir so it survives its source (a temporary mount, a deleted download).
func InstallCA(dir, file, inline string) (string, error) {
	var data []byte
	switch {
	case inline != "":
		data = []byte(inline)
		// Single-line inputs (marketplace forms, some env editors) cannot hold PEM line breaks, so base64 is accepted.
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

// checkPEM fails a wrong file at setup rather than as a TLS error on every connection attempt.
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
