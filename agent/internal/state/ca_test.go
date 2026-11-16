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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testCA(t *testing.T) []byte {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test-ca"}, NotBefore: time.Now(),
		NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestInstallCA(t *testing.T) {
	dir := t.TempDir()
	ca := testCA(t)
	src := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(src, ca, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := InstallCA(dir, src, "")
	if err != nil || got != filepath.Join(dir, CAFileName) {
		t.Fatalf("from file: %q %v", got, err)
	}
	_ = os.Remove(src) // the copy in the state dir must survive the source
	if b, _ := os.ReadFile(got); string(b) != string(ca) {
		t.Fatal("installed CA differs from the source")
	}
	if got, err := InstallCA(dir, got, ""); err != nil || got != filepath.Join(dir, CAFileName) {
		t.Fatalf("re-install from the state copy: %q %v", got, err)
	}
	other := testCA(t)
	if _, err := InstallCA(dir, "/nonexistent", string(other)); err != nil {
		t.Fatalf("inline PEM should win over the file: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, CAFileName)); string(b) != string(other) {
		t.Fatal("inline PEM not installed")
	}
	if _, err := InstallCA(dir, "", base64.StdEncoding.EncodeToString(ca)); err != nil {
		t.Fatalf("base64 PEM: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, CAFileName)); string(b) != string(ca) {
		t.Fatal("base64 PEM not decoded")
	}
	if got, err := InstallCA(dir, "", ""); err != nil || got != "" {
		t.Fatalf("no CA: %q %v", got, err)
	}
	if _, err := InstallCA(dir, "", "not a certificate"); err == nil {
		t.Fatal("garbage accepted as a CA")
	}
}
