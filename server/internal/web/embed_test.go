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
