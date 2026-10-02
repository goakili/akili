// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package fleet

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/audit"
	"github.com/goakili/akili/server/internal/bus"
	"github.com/goakili/akili/server/internal/models"
	"github.com/goakili/akili/server/internal/sessions"
	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"
	"github.com/jkaninda/logger"
	"github.com/jkaninda/wstunnel"
	"gorm.io/gorm"
)

// taskLease mirrors tasks.Lease (kept here to avoid an import cycle).
const taskLease = 3 * time.Minute

// InternalAPI serves the agent-opened streams (LLM gateway) for one agent.
type InternalAPI interface {
	Handler(agentID string) http.Handler
}

// Manager holds the live tunnels on this replica.
type Manager struct {
	db    *gorm.DB
	bus   *bus.Bus
	audit *audit.Logger
	hub   *sessions.Hub
	api   InternalAPI

	mu    sync.Mutex
	conns map[string]*agentConn
}

// NewManager returns a tunnel manager.
func NewManager(db *gorm.DB, b *bus.Bus, a *audit.Logger, hub *sessions.Hub, api InternalAPI) *Manager {
	return &Manager{db: db, bus: b, audit: a, hub: hub, api: api, conns: map[string]*agentConn{}}
}

type agentConn struct {
	m       *Manager
	agentID string
	org     string
	name    string
	sess    *yamux.Session
	control *proto.Conn

	mu        sync.Mutex
	sessions  map[string]*liveSession
	terminals map[string]*liveTerminal
}

type liveSession struct {
	id   string
	conn *proto.Conn
	done bool // a done frame arrived: the end is normal, not a loss
}

// Handle owns an authenticated agent WebSocket until the tunnel closes.
func (m *Manager) Handle(agent *models.Agent, ws *websocket.Conn, version, ip string) {
	sess, err := wstunnel.Client(ws) // this side opens streams
	if err != nil {
		logger.Error("tunnel setup failed", "agent", agent.ID, "error", err)
		_ = ws.Close()
		return
	}
	ac := &agentConn{m: m, agentID: agent.ID, org: agent.OrganizationID, name: agent.Name, sess: sess, sessions: map[string]*liveSession{}, terminals: map[string]*liveTerminal{}}
	m.replace(ac)

	stream, err := sess.OpenStream()
	if err != nil {
		_ = sess.Close()
		return
	}
	ac.control = proto.NewConn(stream)
	if err := ac.control.Send(proto.TypeHello, proto.Hello{AgentID: agent.ID, Name: agent.Name, Labels: agent.Labels,
		MaxParallel: agent.MaxParallel, Draining: agent.Draining}); err != nil {
		_ = sess.Close()
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Streams the agent opens are HTTP requests to the internal API (the yamux session is a
	// net.Listener). They never touch the public listener or its auth.
	internal := &http.Server{Handler: m.api.Handler(agent.ID), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = internal.Serve(sess) }()

	stopCmds := m.bus.SubscribeCommands(ctx, agent.ID, ac.handleCommand)
	go ac.readControl(ctx)

	now := time.Now().UTC()
	m.db.Model(&models.Agent{}).Where("id = ?", agent.ID).Updates(map[string]any{"status": models.AgentOnline, "last_seen_at": now, "version": version})
	m.bus.MarkPresent(ctx, agent.ID, 0)
	m.bus.EmitData(ctx, agent.OrganizationID, bus.Event{Type: "agent.status", AgentID: agent.ID}, map[string]string{"status": models.AgentOnline})
	m.audit.Best(ctx, audit.Entry{OrganizationID: agent.OrganizationID, ActorType: audit.ActorAgent, ActorID: agent.ID,
		Action: "agent.connect", TargetType: "agent", TargetID: agent.ID, IP: ip, Metadata: map[string]any{"version": version}})
	logger.Info("agent connected", "agent", agent.ID, "name", agent.Name, "version", version)
	m.bus.Wake(ctx) // queued tasks may be waiting for this agent
	go ac.resumeTasks(ctx)

	<-sess.CloseChan()
	stopCmds()
	_ = internal.Close()

	ac.mu.Lock()
	lost := make([]*liveSession, 0, len(ac.sessions))
	for _, ls := range ac.sessions {
		lost = append(lost, ls)
	}
	ac.sessions = map[string]*liveSession{}
	ac.mu.Unlock()
	bg := context.Background()
	for _, ls := range lost {
		if !ls.done {
			m.hub.SessionDetached(bg, ls.id, "agent disconnected")
		}
	}
	if m.forget(ac) {
		m.db.Model(&models.Agent{}).Where("id = ? AND status = ?", agent.ID, models.AgentOnline).Update("status", models.AgentOffline)
		m.bus.ClearPresence(bg, agent.ID)
		m.bus.EmitData(bg, agent.OrganizationID, bus.Event{Type: "agent.status", AgentID: agent.ID}, map[string]string{"status": models.AgentOffline})
		m.audit.Best(bg, audit.Entry{OrganizationID: agent.OrganizationID, ActorType: audit.ActorAgent, ActorID: agent.ID,
			Action: "agent.disconnect", TargetType: "agent", TargetID: agent.ID})
		logger.Info("agent disconnected", "agent", agent.ID)
	}
}

// replace installs a new connection, closing a superseded one.
func (m *Manager) replace(ac *agentConn) {
	m.mu.Lock()
	old := m.conns[ac.agentID]
	m.conns[ac.agentID] = ac
	m.mu.Unlock()
	if old != nil {
		_ = old.sess.Close()
	}
}

// forget removes ac if it is still current, and reports whether it was. A superseded connection
// must not mark a freshly reconnected agent offline.
func (m *Manager) forget(ac *agentConn) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conns[ac.agentID] == ac {
		delete(m.conns, ac.agentID)
		return true
	}
	return false
}

// Shutdown closes every tunnel on this replica (agents reconnect to another).
func (m *Manager) Shutdown() {
	m.mu.Lock()
	conns := make([]*agentConn, 0, len(m.conns))
	for _, c := range m.conns {
		conns = append(conns, c)
	}
	m.mu.Unlock()
	for _, c := range conns {
		_ = c.sess.Close()
	}
}

func (ac *agentConn) readControl(ctx context.Context) {
	defer ac.sess.Close()
	var lastWrite time.Time
	for {
		env, err := ac.control.Recv()
		if err != nil {
			return
		}
		if env.Type != proto.TypeHeartbeat {
			continue
		}
		var hb proto.Heartbeat
		if env.Decode(&hb) != nil {
			continue
		}
		ac.m.bus.MarkPresent(ctx, ac.agentID, len(hb.ActiveSessions))
		// A task busy in a long tool call sends no session frames; the heartbeat's list of live
		// sessions is what keeps its lease from expiring.
		if len(hb.ActiveSessions) > 0 {
			ac.m.db.Model(&models.Task{}).Where("assigned_agent_id = ? AND session_id IN ? AND status IN ?", ac.agentID, hb.ActiveSessions,
				[]string{models.TaskAssigned, models.TaskRunning}).Update("lease_until", time.Now().UTC().Add(taskLease))
		}
		if time.Since(lastWrite) > 30*time.Second {
			lastWrite = time.Now()
			now := time.Now().UTC()
			ac.m.db.Model(&models.Agent{}).Where("id = ?", ac.agentID).Select("last_seen_at", "facts", "status").
				Updates(&models.Agent{LastSeenAt: &now, Facts: hb.Facts, Status: models.AgentOnline})
		}
	}
}

func (ac *agentConn) handleCommand(cmd bus.Command) {
	ctx := context.Background()
	switch cmd.Type {
	case bus.CmdUserMessage, bus.CmdStartTask:
		ls, err := ac.ensureSession(ctx, cmd.SessionID)
		if err != nil {
			logger.Warn("cannot open session on agent", "agent", ac.agentID, "session", cmd.SessionID, "error", err)
			ac.m.bus.EmitData(ctx, ac.org, bus.Event{Type: sessions.EvError, SessionID: cmd.SessionID, AgentID: ac.agentID},
				proto.Error{Message: "could not open the session on the agent: " + err.Error()})
			if cmd.Type == bus.CmdStartTask {
				ac.m.hub.SessionLost(ctx, cmd.SessionID, err.Error())
			}
			return
		}
		if cmd.Type == bus.CmdUserMessage {
			var um proto.UserMessage
			if json.Unmarshal(cmd.Data, &um) == nil {
				_ = ls.conn.Send(proto.TypeUserMessage, um)
			}
		}
	case bus.CmdInterrupt:
		if ls := ac.session(cmd.SessionID); ls != nil {
			_ = ls.conn.Send(proto.TypeInterrupt, nil)
		}
	case bus.CmdToolDecision:
		if ls := ac.session(cmd.SessionID); ls != nil {
			var d proto.ToolDecision
			if json.Unmarshal(cmd.Data, &d) == nil {
				_ = ls.conn.Send(proto.TypeToolDecision, d)
			}
		}
	case bus.CmdClose:
		if ls := ac.session(cmd.SessionID); ls != nil {
			ls.done = true
			_ = ls.conn.Close()
		}
	case bus.CmdDrain:
		var body struct {
			Draining bool `json:"draining"`
		}
		_ = json.Unmarshal(cmd.Data, &body)
		_ = ac.control.Send(proto.TypeDrain, body)
	case bus.CmdDisconnect:
		_ = ac.sess.Close()
	case bus.CmdTerminalOpen, bus.CmdTerminalInput, bus.CmdTerminalResize, bus.CmdTerminalClose:
		ac.terminalCommand(cmd)
	}
}

func (ac *agentConn) session(id string) *liveSession {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return ac.sessions[id]
}

// ensureSession returns the live stream for a session, opening it (with history) if needed.
func (ac *agentConn) ensureSession(ctx context.Context, id string) (*liveSession, error) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	if ls, ok := ac.sessions[id]; ok {
		return ls, nil
	}
	open, err := ac.m.hub.BuildOpen(ctx, id, ac.agentID)
	if err != nil {
		return nil, err
	}
	stream, err := ac.sess.OpenStream()
	if err != nil {
		return nil, err
	}
	conn := proto.NewConn(stream)
	if err := conn.Send(proto.TypeSessionOpen, open); err != nil {
		_ = conn.Close()
		return nil, err
	}
	ls := &liveSession{id: id, conn: conn}
	ac.sessions[id] = ls
	go ac.readSession(ls)
	return ls, nil
}

func (ac *agentConn) readSession(ls *liveSession) {
	ctx := context.Background()
	var endErr error
	for {
		env, err := ls.conn.Recv()
		if err != nil {
			endErr = err
			break
		}
		if env.Type == proto.TypeDone {
			ls.done = true
		}
		decision, err := ac.m.hub.HandleFrame(ctx, ac.agentID, ls.id, env)
		if err != nil {
			logger.Warn("rejected session frame", "agent", ac.agentID, "session", ls.id, "type", env.Type, "error", err)
			if errors.Is(err, sessions.ErrNotFound) {
				break
			}
			continue
		}
		if decision != nil {
			if err := ls.conn.Send(proto.TypeToolDecision, decision); err != nil {
				endErr = err
				break
			}
		}
		if ls.done {
			break
		}
	}
	_ = ls.conn.Close()
	ac.mu.Lock()
	if ac.sessions[ls.id] == ls {
		delete(ac.sessions, ls.id)
	}
	ac.mu.Unlock()
	if !ls.done {
		reason := "session stream closed"
		if endErr != nil {
			reason = endErr.Error()
		}
		if ac.sess.IsClosed() {
			ac.m.hub.SessionDetached(ctx, ls.id, "agent disconnected")
		} else {
			ac.m.hub.SessionLost(ctx, ls.id, reason)
		}
	}
}

// resumeTasks reopens the sessions of tasks this agent was running when its tunnel dropped, which
// may have been on another replica. The agent continues from the stored history.
func (ac *agentConn) resumeTasks(ctx context.Context) {
	var ids []string
	ac.m.db.WithContext(ctx).Model(&models.Task{}).
		Where("assigned_agent_id = ? AND status IN ? AND session_id IS NOT NULL", ac.agentID, []string{models.TaskAssigned, models.TaskRunning}).
		Pluck("session_id", &ids)
	for _, id := range ids {
		if ac.session(id) != nil {
			continue
		}
		if _, err := ac.ensureSession(ctx, id); err != nil {
			logger.Warn("cannot resume task session", "agent", ac.agentID, "session", id, "error", err)
			ac.m.hub.SessionLost(ctx, id, "resume failed: "+err.Error())
			continue
		}
		ac.m.audit.Best(ctx, audit.Entry{OrganizationID: ac.org, ActorType: audit.ActorSystem, Action: "session.resume",
			TargetType: "session", TargetID: id, Metadata: map[string]any{"agent_id": ac.agentID}})
		logger.Info("resumed task session", "agent", ac.agentID, "session", id)
	}
}
