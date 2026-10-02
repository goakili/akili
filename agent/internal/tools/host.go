// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package tools

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/goakili/akili/proto"
)

// Host tools run fixed programs with validated arguments (never a shell string), so an input such
// as a unit name cannot smuggle extra commands. Privileged actions go through `sudo -n`, which only
// works for commands the operator allowed in sudoers.

func (e *Executor) hostTool(ctx context.Context, tool string, input json.RawMessage) (string, error) {
	switch tool {
	case proto.ToolServiceStatus:
		var in proto.ServiceInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		return serviceStatus(ctx, unit(in.Unit))
	case proto.ToolServiceRestart:
		var in proto.ServiceInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		u := unit(in.Unit)
		if err := requireLinux("service_restart"); err != nil {
			return "", err
		}
		out, err := runCmd(ctx, 2*time.Minute, "sudo", "-n", "systemctl", "restart", "--", u)
		if err != nil {
			return out, fmt.Errorf("%w (the agent needs a sudoers rule for: systemctl restart %s)", err, u)
		}
		time.Sleep(2 * time.Second) // let the unit settle before reporting its state
		st, _ := serviceStatus(ctx, u)
		return "restarted " + u + "\n\n" + st, nil
	case proto.ToolJournalLogs:
		var in proto.JournalInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		return journal(ctx, in)
	case proto.ToolDiskUsage:
		var in proto.DiskUsageInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		return e.diskUsage(ctx, in)
	case proto.ToolProcessList:
		var in proto.ProcessListInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		return processList(ctx, in)
	case proto.ToolDockerPS:
		var in proto.DockerPSInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		args := []string{"ps", "--format", "table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}"}
		if in.All {
			args = append(args, "--all")
		}
		return runCmd(ctx, 30*time.Second, "docker", args...)
	case proto.ToolDockerLogs:
		var in proto.ContainerInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		lines := clamp(in.Lines, 200, 2000)
		args := []string{"logs", "--timestamps", "--tail", strconv.Itoa(lines)}
		if in.Since != "" {
			args = append(args, "--since", in.Since)
		}
		return runCmd(ctx, 30*time.Second, "docker", append(args, "--", in.Container)...)
	case proto.ToolDockerRestart:
		var in proto.ContainerInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		if out, err := runCmd(ctx, 2*time.Minute, "docker", "restart", "--", in.Container); err != nil {
			return out, err
		}
		time.Sleep(2 * time.Second)
		return runCmd(ctx, 30*time.Second, "docker", "ps", "--all", "--filter", "name=^"+in.Container+"$", "--format", "{{.Names}}: {{.Status}}")
	case proto.ToolCertCheck:
		var in proto.HostPortInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		return certCheck(ctx, in)
	case proto.ToolPackageUpdates:
		return packageUpdates(ctx)
	case proto.ToolNetProbe:
		var in proto.HostPortInput
		if err := json.Unmarshal(input, &in); err != nil {
			return "", err
		}
		return netProbe(ctx, in)
	}
	return "", fmt.Errorf("unknown host tool %s", tool)
}

func unit(u string) string {
	u = strings.TrimSpace(u)
	if !strings.Contains(u, ".") {
		u += ".service"
	}
	return u
}

func requireLinux(tool string) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("%s needs systemd (Linux); this agent runs on %s", tool, runtime.GOOS)
	}
	return nil
}

// run executes a program with a timeout and a minimal environment.
func runCmd(ctx context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%s is not installed on this host", name)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/opt/homebrew/bin:/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C.UTF-8", "SYSTEMD_PAGER=", "PAGER=cat", "SYSTEMD_COLORS=0"}
	var out bytes.Buffer
	w := &capWriter{buf: &out, max: MaxOutput * 2}
	cmd.Stdout, cmd.Stderr = w, w
	err = cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return out.String(), fmt.Errorf("timed out after %s", timeout)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return out.String(), fmt.Errorf("exit status %d", exitErr.ExitCode())
	}
	return out.String(), err
}

func serviceStatus(ctx context.Context, u string) (string, error) {
	if err := requireLinux("service_status"); err != nil {
		return "", err
	}
	out, err := runCmd(ctx, 30*time.Second, "systemctl", "status", "--no-pager", "--lines=20", "--", u)
	// systemctl status exits 3 for inactive units: that is an answer, not a failure.
	if err != nil && strings.Contains(err.Error(), "exit status 3") {
		return out, nil
	}
	return out, err
}

func journal(ctx context.Context, in proto.JournalInput) (string, error) {
	if err := requireLinux("journal_logs"); err != nil {
		return "", err
	}
	args := []string{"--no-pager", "--output=short-iso", "--lines=" + strconv.Itoa(clamp(in.Lines, 200, 2000))}
	if in.Unit != "" {
		args = append(args, "--unit="+unit(in.Unit))
	}
	if in.Since != "" {
		args = append(args, "--since="+in.Since)
	}
	if in.Grep != "" {
		args = append(args, "--grep="+in.Grep)
	}
	out, err := runCmd(ctx, 45*time.Second, "journalctl", args...)
	if err == nil && strings.TrimSpace(out) == "" {
		return "no log lines matched", nil
	}
	return out, err
}

func (e *Executor) diskUsage(ctx context.Context, in proto.DiskUsageInput) (string, error) {
	path := in.Path
	if path == "" {
		path = "/"
	}
	real, err := e.resolve(path)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	df, err := runCmd(ctx, 30*time.Second, "df", "-h")
	if err != nil {
		return df, err
	}
	b.WriteString(df)
	// Largest entries one level down, staying on one filesystem; slow trees are cut off by the timeout.
	du, _ := runCmd(ctx, 60*time.Second, "du", "-x", "-k", "-d", "1", real)
	type entry struct {
		kb   int64
		path string
	}
	var entries []entry
	for _, line := range strings.Split(du, "\n") {
		f := strings.SplitN(line, "\t", 2)
		if len(f) != 2 {
			continue
		}
		kb, err := strconv.ParseInt(strings.TrimSpace(f[0]), 10, 64)
		if err == nil {
			entries = append(entries, entry{kb, f[1]})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].kb > entries[j].kb })
	fmt.Fprintf(&b, "\nLargest under %s:\n", real)
	for i, en := range entries {
		if i >= 15 {
			break
		}
		fmt.Fprintf(&b, "%8s  %s\n", human(en.kb*1024), en.path)
	}
	return b.String(), nil
}

func human(n int64) string {
	units := []string{"B", "K", "M", "G", "T"}
	f := float64(n)
	i := 0
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	return fmt.Sprintf("%.1f%s", f, units[i])
}

func processList(ctx context.Context, in proto.ProcessListInput) (string, error) {
	sortKey := "-%cpu"
	if in.Sort == "mem" {
		sortKey = "-%mem"
	}
	limit := clamp(in.Limit, 15, 100)
	args := []string{"-eo", "pid,user,%cpu,%mem,rss,etime,comm"}
	if runtime.GOOS == "linux" {
		args = append(args, "--sort="+sortKey)
	} else {
		args = []string{"-Aro", "pid,user,%cpu,%mem,rss,etime,comm"}
		if in.Sort == "mem" {
			args[0] = "-Amo"
		}
	}
	out, err := runCmd(ctx, 20*time.Second, "ps", args...)
	if err != nil {
		return out, err
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) > limit+1 {
		lines = lines[:limit+1]
	}
	return strings.Join(lines, "\n"), nil
}

func certCheck(ctx context.Context, in proto.HostPortInput) (string, error) {
	port := in.Port
	if port == 0 {
		port = 443
	}
	d := &tls.Dialer{NetDialer: &net.Dialer{Timeout: 10 * time.Second}, Config: &tls.Config{ServerName: in.Host, InsecureSkipVerify: true}} //nolint:gosec // we report on the chain, verifying it below
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(in.Host, strconv.Itoa(port)))
	if err != nil {
		return "", err
	}
	defer conn.Close()
	state := conn.(*tls.Conn).ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return "", errors.New("no certificate presented")
	}
	var b strings.Builder
	leaf := state.PeerCertificates[0]
	inter := x509.NewCertPool()
	for _, c := range state.PeerCertificates[1:] {
		inter.AddCert(c)
	}
	_, verr := leaf.Verify(x509.VerifyOptions{DNSName: in.Host, Intermediates: inter})
	days := int(time.Until(leaf.NotAfter).Hours() / 24)
	fmt.Fprintf(&b, "%s:%d  TLS %s\n", in.Host, port, tls.VersionName(state.Version))
	fmt.Fprintf(&b, "subject:  %s\nissuer:   %s\nnames:    %s\nvalid:    %s → %s (%d days left)\n",
		leaf.Subject.CommonName, leaf.Issuer.CommonName, strings.Join(leaf.DNSNames, ", "),
		leaf.NotBefore.Format("2006-01-02"), leaf.NotAfter.Format("2006-01-02"), days)
	if verr != nil {
		fmt.Fprintf(&b, "trust:    NOT TRUSTED: %v\n", verr)
	} else {
		b.WriteString("trust:    chain verifies\n")
	}
	switch {
	case days < 0:
		b.WriteString("status:   EXPIRED\n")
	case days < 14:
		b.WriteString("status:   EXPIRES SOON\n")
	default:
		b.WriteString("status:   ok\n")
	}
	return b.String(), nil
}

func packageUpdates(ctx context.Context) (string, error) {
	switch {
	case has("apt"):
		return runCmd(ctx, 2*time.Minute, "apt", "list", "--upgradable")
	case has("dnf"):
		out, err := runCmd(ctx, 3*time.Minute, "dnf", "--quiet", "check-update")
		if err != nil && strings.Contains(err.Error(), "exit status 100") { // 100 = updates available
			err = nil
		}
		return out, err
	case has("yum"):
		out, err := runCmd(ctx, 3*time.Minute, "yum", "--quiet", "check-update")
		if err != nil && strings.Contains(err.Error(), "exit status 100") {
			err = nil
		}
		return out, err
	case has("apk"):
		return runCmd(ctx, 2*time.Minute, "apk", "version", "-l", "<")
	case has("brew"):
		return runCmd(ctx, 2*time.Minute, "brew", "outdated")
	}
	return "", errors.New("no supported package manager found (apt, dnf, yum, apk, brew)")
}

func has(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func netProbe(ctx context.Context, in proto.HostPortInput) (string, error) {
	var b strings.Builder
	start := time.Now()
	ips, err := net.DefaultResolver.LookupHost(ctx, in.Host)
	if err != nil {
		return "", fmt.Errorf("DNS lookup failed: %w", err)
	}
	fmt.Fprintf(&b, "resolved %s → %s (%s)\n", in.Host, strings.Join(ips, ", "), time.Since(start).Round(time.Millisecond))
	start = time.Now()
	conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(in.Host, strconv.Itoa(in.Port)))
	if err != nil {
		fmt.Fprintf(&b, "tcp %d: FAILED after %s: %v\n", in.Port, time.Since(start).Round(time.Millisecond), err)
		return b.String(), fmt.Errorf("port %d unreachable", in.Port)
	}
	_ = conn.Close()
	fmt.Fprintf(&b, "tcp %d: open (%s, via %s)\n", in.Port, time.Since(start).Round(time.Millisecond), conn.RemoteAddr())
	return b.String(), nil
}

func clamp(v, def, max int) int {
	if v <= 0 {
		return def
	}
	if v > max {
		return max
	}
	return v
}
