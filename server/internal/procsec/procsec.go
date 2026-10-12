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

// Package procsec hardens the control-plane process itself.
package procsec

// NoDump marks the process non-dumpable. Its /proc files (environ, mem, fds) then belong to root,
// so child processes running as the same user, such as stdio MCP servers, cannot read the
// control plane's secrets from them. It also disables core dumps, which could contain secrets.
// It is a no-op outside Linux.
func NoDump() error { return noDump() }
