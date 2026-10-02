// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package procsec

import "golang.org/x/sys/unix"

func noDump() error { return unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0) }
