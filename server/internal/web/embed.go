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

// Package web embeds the built UI and the agent install script; dist/ is built by make web (only .gitkeep is committed).
package web

import (
	"bytes"
	"embed"
)

//go:embed all:dist
var Assets embed.FS

//go:embed install-agent.sh
var installScript []byte

// InstallScript is served at /install-agent.sh and installs the agent release version (see config.AgentVersion).
func InstallScript(version string) []byte {
	return bytes.ReplaceAll(installScript, []byte("__AKILI_AGENT_VERSION__"), []byte(version))
}
