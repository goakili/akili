// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package procsec hardens the agent process itself.
package procsec

// NoDump marks the process non-dumpable. Its /proc files (environ, mem, fds) then belong to root,
// so the commands the agent runs as the same user cannot read its session credentials from them.
// It also disables core dumps. It is a no-op outside Linux.
func NoDump() error { return noDump() }
