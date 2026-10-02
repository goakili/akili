// SPDX-FileCopyrightText: 2026 Jonas Kaninda
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
