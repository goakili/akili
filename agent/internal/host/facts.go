// Copyright (c) 2026 Jonas Kaninda.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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
