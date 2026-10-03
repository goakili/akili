// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"bytes"
	"testing"
)

func TestInstallScriptVersion(t *testing.T) {
	s := InstallScript("0.0.2")
	if !bytes.Contains(s, []byte(`AKILI_AGENT_VERSION:-0.0.2}`)) || bytes.Contains(s, []byte("__AKILI_AGENT_VERSION__")) {
		t.Fatal("version not filled into the install script")
	}
}
