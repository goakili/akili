// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package procsec hardens the control-plane process itself.
package procsec

// NoDump marks the process non-dumpable. Its /proc files (environ, mem, fds) then belong to root,
// so child processes running as the same user, such as stdio MCP servers, cannot read the
// control plane's secrets from them. It also disables core dumps, which could contain secrets.
// It is a no-op outside Linux.
func NoDump() error { return noDump() }
