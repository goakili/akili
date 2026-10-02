// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build unix

package mcp

import "syscall"

// writable reports whether this process may write path (as the kernel decides, ACLs included).
func writable(path string) bool { return syscall.Access(path, wOK) == nil }

const wOK = 0x2 // W_OK, which package syscall does not export on every platform
