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

// Package web embeds the built UI and the agent install script into the server binary. dist/ is a
// build artifact (make web); only a .gitkeep is committed so `go build` works without the UI.
package web

import (
	"bytes"
	"embed"
)

// Assets holds the built SPA under "dist/", served with Okapi's WebFS.
//
//go:embed all:dist
var Assets embed.FS

//go:embed install-agent.sh
var installScript []byte

// InstallScript is served at /install-agent.sh. It installs agent release version (see
// config.AgentVersion).
func InstallScript(version string) []byte {
	return bytes.ReplaceAll(installScript, []byte("__AKILI_AGENT_VERSION__"), []byte(version))
}
