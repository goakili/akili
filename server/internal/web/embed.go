// SPDX-FileCopyrightText: 2026 Jonas Kaninda
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
