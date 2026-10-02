// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package fleet

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/audit"
	"github.com/goakili/akili/server/internal/bus"
	"github.com/goakili/akili/server/internal/models"
	"github.com/jkaninda/logger"
)

// MaxRecording bounds a stored terminal recording; the rest is dropped and the session marked truncated.
const MaxRecording = 2 << 20

// liveTerminal is a terminal whose agent stream this replica holds. It records everything (output,
// operator input and resizes) as an asciinema v2 cast.
type liveTerminal struct {
	id    string
	conn  *proto.Conn
	start time.Time

	mu        sync.Mutex
	cast      strings.Builder
	bytes     int
	truncated bool
}

func (t *liveTerminal) record(kind, data string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.bytes += len(data)
	if t.cast.Len() > MaxRecording {
		t.truncated = true
		return
	}
	ev, _ := json.Marshal([]any{time.Since(t.start).Seconds(), kind, data})
	t.cast.Write(ev)
	t.cast.WriteByte('\n')
}

func (ac *agentConn) terminal(id string) *liveTerminal {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return ac.terminals[id]
}

// openTerminal opens a pty stream to the agent and relays its output to the browser's replica.
func (ac *agentConn) openTerminal(ctx context.Context, id string, req bus.TerminalOpen) {
	fail := func(msg string) {
		ac.m.bus.PublishTerminal(ctx, id, bus.TerminalFrame{Type: "exit", Code: -1, Err: msg})
		ac.m.finishTerminal(ctx, id, nil, -1)
	}
	pol, signed, err := ac.m.hub.SignedAgentPolicy(ctx, ac.agentID)
	if err != nil || !pol.Terminal {
		fail("the agent's policy does not allow terminals")
		return
	}
	stream, err := ac.sess.OpenStream()
	if err != nil {
		fail("could not reach the agent")
		return
	}
	conn := proto.NewConn(stream)
	if err := conn.Send(proto.TypePTYOpen, proto.PTYOpen{TerminalID: id, Cols: req.Cols, Rows: req.Rows, Policy: signed, UserID: req.UserID}); err != nil {
		fail("could not reach the agent")
		return
	}
	t := &liveTerminal{id: id, conn: conn, start: time.Now()}
	header, _ := json.Marshal(map[string]any{"version": 2, "width": req.Cols, "height": req.Rows, "timestamp": t.start.Unix(),
		"env": map[string]string{"TERM": "xterm-256color"}, "title": "akili " + ac.name})
	t.cast.Write(header)
	t.cast.WriteByte('\n')
	ac.mu.Lock()
	ac.terminals[id] = t
	ac.mu.Unlock()
	go func() {
		code := 0
		for {
			env, err := conn.Recv()
			if err != nil {
				break
			}
			switch env.Type {
			case proto.TypePTYOutput:
				var d proto.PTYData
				if env.Decode(&d) == nil {
					t.record("o", string(d.Data))
					ac.m.bus.PublishTerminal(ctx, id, bus.TerminalFrame{Type: "output", Data: d.Data})
				}
			case proto.TypePTYExit:
				var x proto.PTYExit
				_ = env.Decode(&x)
				code = x.Code
				ac.m.bus.PublishTerminal(ctx, id, bus.TerminalFrame{Type: "exit", Code: x.Code, Err: x.Error})
			}
		}
		_ = conn.Close()
		ac.mu.Lock()
		delete(ac.terminals, id)
		ac.mu.Unlock()
		ac.m.bus.PublishTerminal(ctx, id, bus.TerminalFrame{Type: "exit", Code: code})
		ac.m.finishTerminal(ctx, id, t, code)
	}()
}

// finishTerminal stores the recording and audits the end of the terminal.
func (m *Manager) finishTerminal(ctx context.Context, id string, t *liveTerminal, code int) {
	now := time.Now().UTC()
	up := map[string]any{"status": "closed", "ended_at": now, "exit_code": code}
	if t != nil {
		t.mu.Lock()
		up["recording"], up["bytes"], up["truncated"] = t.cast.String(), t.bytes, t.truncated
		t.mu.Unlock()
	}
	var ts models.TerminalSession
	if m.db.WithContext(ctx).First(&ts, "id = ?", id).Error != nil {
		return
	}
	if ts.Status == "closed" {
		return
	}
	m.db.WithContext(ctx).Model(&ts).Updates(up)
	m.audit.Best(ctx, audit.Entry{OrganizationID: ts.OrganizationID, ActorType: audit.ActorUser, ActorID: ts.UserID, Action: "terminal.close",
		TargetType: "terminal", TargetID: id, Metadata: map[string]any{"agent_id": ts.AgentID, "exit_code": code,
			"duration": fmt.Sprint(now.Sub(ts.CreatedAt).Round(time.Second))}})
	logger.Info("terminal closed", "terminal", id, "agent", ts.AgentID)
}

func (ac *agentConn) terminalCommand(cmd bus.Command) {
	ctx := context.Background()
	switch cmd.Type {
	case bus.CmdTerminalOpen:
		var req bus.TerminalOpen
		if json.Unmarshal(cmd.Data, &req) == nil {
			ac.openTerminal(ctx, cmd.SessionID, req)
		}
	case bus.CmdTerminalInput:
		if t := ac.terminal(cmd.SessionID); t != nil {
			var d proto.PTYData
			if json.Unmarshal(cmd.Data, &d) == nil {
				t.record("i", string(d.Data))
				_ = t.conn.Send(proto.TypePTYInput, d)
			}
		}
	case bus.CmdTerminalResize:
		if t := ac.terminal(cmd.SessionID); t != nil {
			var r proto.PTYResize
			if json.Unmarshal(cmd.Data, &r) == nil {
				t.record("r", fmt.Sprintf("%dx%d", r.Cols, r.Rows))
				_ = t.conn.Send(proto.TypePTYResize, r)
			}
		}
	case bus.CmdTerminalClose:
		if t := ac.terminal(cmd.SessionID); t != nil {
			_ = t.conn.Close()
		}
	}
}
