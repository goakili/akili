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

// Package host collects facts about the machine the agent runs on.
package host

import (
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/goakili/akili/proto"
)

// Facts gathers host facts. Linux values come from /proc; elsewhere only the basics are reported.
func Facts(version, workdir string) proto.HostFacts {
	name, _ := os.Hostname()
	f := proto.HostFacts{Hostname: name, OS: runtime.GOOS, Arch: runtime.GOARCH, CPUs: runtime.NumCPU(), AgentVersion: version, Workdir: workdir}
	if b, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		f.Kernel = strings.TrimSpace(string(b))
	}
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			kb, _ := strconv.ParseUint(fields[1], 10, 64)
			switch fields[0] {
			case "MemTotal:":
				f.MemTotalMB = kb / 1024
			case "MemAvailable:":
				f.MemAvailMB = kb / 1024
			}
		}
	}
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		if fields := strings.Fields(string(b)); len(fields) > 0 {
			f.Load1, _ = strconv.ParseFloat(fields[0], 64)
		}
	}
	if b, err := os.ReadFile("/proc/uptime"); err == nil {
		if fields := strings.Fields(string(b)); len(fields) > 0 {
			up, _ := strconv.ParseFloat(fields[0], 64)
			f.UptimeSec = uint64(up)
		}
	}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() && !ipn.IP.IsLinkLocalUnicast() {
				f.IPs = append(f.IPs, ipn.IP.String())
			}
		}
	}
	return f
}
