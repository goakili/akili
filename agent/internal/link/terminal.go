// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package link

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/goakili/akili/proto"
	"github.com/jkaninda/logger"
)

// maxTerminal bounds one interactive terminal.
const maxTerminal = 2 * time.Hour

// terminal runs an interactive shell for an operator. The control plane only opens one for admins
// on agents whose policy allows it; the agent checks that same policy, signed, before starting.
func (a *Agent) terminal(ctx context.Context, conn *proto.Conn, env proto.Envelope) {
	defer conn.Close()
	var open proto.PTYOpen
	if err := env.Decode(&open); err != nil {
		return
	}
	fail := func(msg string) { _ = conn.Send(proto.TypePTYExit, proto.PTYExit{Code: -1, Error: msg}) }
	pol, err := open.Policy.Verify(a.st.CPSigningKey)
	if err != nil {
		fail("terminal refused: policy signature invalid")
		return
	}
	if !pol.Terminal {
		fail("terminal refused: the agent's policy does not allow terminals")
		return
	}
	shell := "/bin/bash"
	if _, err := os.Stat(shell); err != nil {
		shell = "/bin/sh"
	}
	ctx, cancel := context.WithTimeout(ctx, maxTerminal)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, "-i")
	cmd.Dir = a.st.Workdir
	cmd.Env = append([]string{
		"PATH=/usr/local/sbin:/usr/local/bin:/opt/homebrew/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=" + a.st.Workdir, "TERM=xterm-256color", "LANG=C.UTF-8", "AKILI_TERMINAL=" + open.TerminalID,
	}, a.ShellEnv...)
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(clampSize(open.Cols, 80)), Rows: uint16(clampSize(open.Rows, 24))})
	if err != nil {
		fail("could not start a shell: " + err.Error())
		return
	}
	defer f.Close()
	logger.Info("terminal opened", "terminal", open.TerminalID, "user", open.UserID)

	var once sync.Once
	done := make(chan struct{})
	finish := func() { once.Do(func() { close(done) }) }
	go func() { // operator → shell
		defer finish()
		for {
			env, err := conn.Recv()
			if err != nil {
				return
			}
			switch env.Type {
			case proto.TypePTYInput:
				var d proto.PTYData
				if env.Decode(&d) == nil {
					_, _ = f.Write(d.Data)
				}
			case proto.TypePTYResize:
				var r proto.PTYResize
				if env.Decode(&r) == nil {
					_ = pty.Setsize(f, &pty.Winsize{Cols: uint16(clampSize(r.Cols, 80)), Rows: uint16(clampSize(r.Rows, 24))})
				}
			}
		}
	}()
	go func() { // shell → operator
		defer finish()
		buf := make([]byte, 32<<10)
		for {
			n, err := f.Read(buf)
			if n > 0 {
				if conn.Send(proto.TypePTYOutput, proto.PTYData{Data: append([]byte(nil), buf[:n]...)}) != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	<-done
	cancel()
	_ = cmd.Process.Kill()
	werr := cmd.Wait()
	code := 0
	var exitErr *exec.ExitError
	if errors.As(werr, &exitErr) {
		code = exitErr.ExitCode()
	}
	_ = conn.Send(proto.TypePTYExit, proto.PTYExit{Code: code})
	logger.Info("terminal closed", "terminal", open.TerminalID)
}

func clampSize(v, def int) int {
	if v <= 0 || v > 1000 {
		return def
	}
	return v
}
