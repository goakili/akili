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

// Package bus carries events to browsers and commands to the replica holding an agent's tunnel, over Redis pub/sub.
package bus

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jkaninda/logger"
	"github.com/redis/go-redis/v9"
)

type Event struct {
	Type      string          `json:"type"`
	SessionID string          `json:"session_id,omitempty"`
	AgentID   string          `json:"agent_id,omitempty"`
	TaskID    string          `json:"task_id,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
	TS        time.Time       `json:"ts"`
}

// Command is sent to the replica holding an agent tunnel.
type Command struct {
	Type      string          `json:"type"`
	SessionID string          `json:"session_id,omitempty"`
	TaskID    string          `json:"task_id,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
}

const (
	CmdUserMessage  = "session.user_message" // Data: proto.UserMessage
	CmdInterrupt    = "session.interrupt"
	CmdClose        = "session.close"
	CmdToolDecision = "session.tool_decision" // Data: proto.ToolDecision
	CmdStartTask    = "task.start"
	CmdDisconnect   = "agent.disconnect"
	CmdDrain        = "agent.drain" // Data: {"draining": bool}
	// Terminal relay: the browser's replica sends these to the replica holding the tunnel.
	CmdTerminalOpen   = "terminal.open"   // Data: TerminalOpen
	CmdTerminalInput  = "terminal.input"  // Data: proto.PTYData
	CmdTerminalResize = "terminal.resize" // Data: proto.PTYResize
	CmdTerminalClose  = "terminal.close"
)

// TerminalOpen asks the tunnel replica to open a terminal. SessionID carries the terminal id.
type TerminalOpen struct {
	Cols   int    `json:"cols"`
	Rows   int    `json:"rows"`
	UserID string `json:"user_id"`
}

// TerminalFrame flows from the tunnel replica to the browser's replica.
type TerminalFrame struct {
	Type string `json:"type"` // output | exit
	Data []byte `json:"data,omitempty"`
	Code int    `json:"code,omitempty"`
	Err  string `json:"error,omitempty"`
}

func terminalChannel(id string) string { return "akili:term:" + id }

func (b *Bus) PublishTerminal(ctx context.Context, id string, f TerminalFrame) {
	payload, _ := json.Marshal(f)
	b.rdb.Publish(ctx, terminalChannel(id), payload)
}

// SubscribeTerminal receives a terminal's frames. The subscription is active when it returns.
func (b *Bus) SubscribeTerminal(ctx context.Context, id string) (<-chan TerminalFrame, func()) {
	ps := b.rdb.Subscribe(ctx, terminalChannel(id))
	_, _ = ps.Receive(ctx)
	out := make(chan TerminalFrame, 256)
	done := make(chan struct{})
	go func() {
		defer close(out)
		ch := ps.Channel()
		for {
			select {
			case <-done:
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				var f TerminalFrame
				if json.Unmarshal([]byte(msg.Payload), &f) == nil {
					select {
					case out <- f:
					case <-done:
						return
					}
				}
			}
		}
	}()
	return out, func() { close(done); _ = ps.Close() }
}

// ErrAgentOffline means no replica holds the agent's tunnel.
var ErrAgentOffline = errors.New("agent is not connected")

type Bus struct {
	rdb *redis.Client
}

func New(rdb *redis.Client) *Bus { return &Bus{rdb: rdb} }

func orgChannel(org string) string        { return "akili:events:org:" + org }
func sessionChannel(id string) string     { return "akili:events:session:" + id }
func agentCmdChannel(agent string) string { return "akili:cmd:agent:" + agent }

// Emit publishes an event to the organization feed and, when it belongs to a session, the session feed.
func (b *Bus) Emit(ctx context.Context, org string, ev Event) {
	if ev.TS.IsZero() {
		ev.TS = time.Now().UTC()
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		return
	}
	pipe := b.rdb.Pipeline()
	pipe.Publish(ctx, orgChannel(org), payload)
	if ev.SessionID != "" {
		pipe.Publish(ctx, sessionChannel(ev.SessionID), payload)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		logger.Warn("event publish failed", "type", ev.Type, "error", err)
	}
}

func (b *Bus) EmitData(ctx context.Context, org string, ev Event, data any) {
	if data != nil {
		ev.Data, _ = json.Marshal(data)
	}
	b.Emit(ctx, org, ev)
}

type Subscription struct {
	C     <-chan Event
	close func()
}

func (s *Subscription) Close() { s.close() }

func (b *Bus) SubscribeOrg(ctx context.Context, org string) *Subscription {
	return b.subscribe(ctx, orgChannel(org))
}

func (b *Bus) SubscribeSession(ctx context.Context, id string) *Subscription {
	return b.subscribe(ctx, sessionChannel(id))
}

func (b *Bus) subscribe(ctx context.Context, channel string) *Subscription {
	ps := b.rdb.Subscribe(ctx, channel)
	out := make(chan Event, 256)
	done := make(chan struct{})
	go func() {
		defer close(out)
		ch := ps.Channel()
		for {
			select {
			case <-done:
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				var ev Event
				if json.Unmarshal([]byte(msg.Payload), &ev) != nil {
					continue
				}
				select {
				case out <- ev:
				default: // a slow browser drops events rather than blocking the fan-out
				}
			}
		}
	}()
	return &Subscription{C: out, close: func() {
		close(done)
		_ = ps.Close()
	}}
}

// SendCommand returns ErrAgentOffline when PUBLISH reaches no subscriber: no replica holds the tunnel.
func (b *Bus) SendCommand(ctx context.Context, agentID string, cmd Command) error {
	payload, err := json.Marshal(cmd)
	if err != nil {
		return err
	}
	n, err := b.rdb.Publish(ctx, agentCmdChannel(agentID), payload).Result()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrAgentOffline
	}
	return nil
}

func (b *Bus) SendCommandData(ctx context.Context, agentID string, cmd Command, data any) error {
	if data != nil {
		var err error
		if cmd.Data, err = json.Marshal(data); err != nil {
			return err
		}
	}
	return b.SendCommand(ctx, agentID, cmd)
}

// SubscribeCommands receives commands for an agent. Only the replica holding its tunnel subscribes.
func (b *Bus) SubscribeCommands(ctx context.Context, agentID string, handle func(Command)) (stop func()) {
	ps := b.rdb.Subscribe(ctx, agentCmdChannel(agentID))
	// Wait for the subscription to be active so a command sent right after connect is not lost.
	_, _ = ps.Receive(ctx)
	done := make(chan struct{})
	go func() {
		ch := ps.Channel()
		for {
			select {
			case <-done:
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				var cmd Command
				if json.Unmarshal([]byte(msg.Payload), &cmd) == nil {
					handle(cmd)
				}
			}
		}
	}()
	return func() {
		close(done)
		_ = ps.Close()
	}
}

func presenceKey(agentID string) string { return "akili:presence:agent:" + agentID }

// PresenceTTL is how long an agent stays "online" without a heartbeat.
const PresenceTTL = 45 * time.Second

func (b *Bus) MarkPresent(ctx context.Context, agentID string, activeSessions int) {
	b.rdb.Set(ctx, presenceKey(agentID), activeSessions, PresenceTTL)
}

func (b *Bus) ClearPresence(ctx context.Context, agentID string) {
	b.rdb.Del(ctx, presenceKey(agentID))
}

func (b *Bus) Present(ctx context.Context, agentID string) bool {
	n, err := b.rdb.Exists(ctx, presenceKey(agentID)).Result()
	return err == nil && n == 1
}

// PresentMany reports which agents are connected, in one round trip.
func (b *Bus) PresentMany(ctx context.Context, agentIDs []string) map[string]bool {
	out := make(map[string]bool, len(agentIDs))
	if len(agentIDs) == 0 {
		return out
	}
	keys := make([]string, len(agentIDs))
	for i, id := range agentIDs {
		keys[i] = presenceKey(id)
	}
	vals, err := b.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return out
	}
	for i, v := range vals {
		out[agentIDs[i]] = v != nil
	}
	return out
}

// UseNonce records a handshake nonce and reports whether it was fresh (replay protection).
func (b *Bus) UseNonce(ctx context.Context, agentID, nonce string, ttl time.Duration) bool {
	ok, err := b.rdb.SetNX(ctx, "akili:nonce:"+agentID+":"+nonce, 1, ttl).Result()
	return err == nil && ok
}

// Wake nudges the dispatcher (any replica leading it).
func (b *Bus) Wake(ctx context.Context) {
	b.rdb.Publish(ctx, "akili:dispatch:wake", "1")
}

func (b *Bus) SubscribeWake(ctx context.Context) <-chan *redis.Message {
	return b.rdb.Subscribe(ctx, "akili:dispatch:wake").Channel()
}

func (b *Bus) Redis() *redis.Client { return b.rdb }
