// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package chat

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/audit"
	"github.com/goakili/akili/server/internal/auth"
	"github.com/goakili/akili/server/internal/bus"
	"github.com/goakili/akili/server/internal/crypto"
	"github.com/goakili/akili/server/internal/models"
	"github.com/goakili/akili/server/internal/sessions"
	"github.com/goakili/akili/server/internal/tasks"
	"github.com/jkaninda/logger"
	"gorm.io/gorm"
)

// Leader reports whether this replica runs singleton loops (pollers and the reply relay).
type Leader interface{ Leading() bool }

// Service runs the chat gateways.
type Service struct {
	db     *gorm.DB
	box    *crypto.Box
	bus    *bus.Bus
	audit  *audit.Logger
	hub    *sessions.Hub
	tasks  *tasks.Service
	leader Leader
}

// New returns the service.
func New(db *gorm.DB, box *crypto.Box, b *bus.Bus, a *audit.Logger, hub *sessions.Hub, t *tasks.Service, l Leader) *Service {
	return &Service{db: db, box: box, bus: b, audit: a, hub: hub, tasks: t, leader: l}
}

// LinkCodeTTL is how long a link code is valid.
const LinkCodeTTL = 10 * time.Minute

// Trigger marks tasks started from chat; TriggerRef is the conversation id.
const Trigger = "chat"

// PlatformFor builds the client for a channel.
func (s *Service) PlatformFor(ch *models.ChatChannel) (Platform, error) {
	token, err := s.box.Decrypt(ch.TokenEnc)
	if err != nil {
		return nil, fmt.Errorf("channel credentials: %w", err)
	}
	switch ch.Kind {
	case models.ChatTelegram:
		return NewTelegram(ch.APIBaseURL, token), nil
	case models.ChatSlack:
		secret, err := s.box.Decrypt(ch.SecretEnc)
		if err != nil {
			return nil, fmt.Errorf("channel credentials: %w", err)
		}
		return NewSlack(ch.APIBaseURL, token, secret), nil
	case models.ChatSignal:
		return NewSignal(ch.APIBaseURL, ch.Account), nil
	}
	return nil, fmt.Errorf("unknown chat kind %q", ch.Kind)
}

// NewLinkCode returns a one-time code the user sends to the bot ("/link <code>") to link their chat
// account. Only its hash is stored.
func (s *Service) NewLinkCode(ctx context.Context, org, userID string) (string, time.Time, error) {
	code := "AK-" + strings.ToUpper(rand.Text()[:10])
	exp := time.Now().UTC().Add(LinkCodeTTL)
	err := s.db.WithContext(ctx).Create(&models.ChatLinkCode{Base: models.Base{ID: models.NewID("clc"), OrganizationID: org},
		UserID: userID, Hash: crypto.HashToken(code), ExpiresAt: exp}).Error
	return code, exp, err
}

// reply sends to a chat and records a failure on the channel.
func (s *Service) reply(ctx context.Context, ch *models.ChatChannel, chatID string, msg Outgoing) {
	p, err := s.PlatformFor(ch)
	if err == nil {
		err = p.Send(ctx, chatID, msg)
	}
	if err != nil {
		logger.Warn("chat reply failed", "channel", ch.ID, "kind", ch.Kind, "error", err)
		s.db.WithContext(ctx).Model(&models.ChatChannel{}).Where("id = ?", ch.ID).UpdateColumn("last_error", truncate(err.Error(), 480))
	}
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

const helpText = `Akili commands:
/link <code>   link this chat account to your Akili user (get a code in Akili → Chat)
/agents        list agents
/agent <name>  talk to another agent (starts a new conversation)
/new           start a new conversation with the current agent
/task <goal>   run a task on the current agent; the result is posted here
/status        current agent and pending approvals
/approvals     list pending approvals
/approve <id>, /deny <id>
/unlink        unlink this chat account
Anything else is sent to the agent.`

// Handle processes one incoming message or button press. It never returns an error to the platform:
// problems are answered in the chat.
func (s *Service) Handle(ctx context.Context, ch *models.ChatChannel, in Incoming) {
	if !auth.RateLimit(ctx, s.bus.Redis(), "chat:"+ch.ID+":"+in.UserID, 30, time.Minute, false) {
		s.reply(ctx, ch, in.ChatID, Outgoing{Text: "Too many messages; wait a minute."})
		return
	}
	s.db.WithContext(ctx).Model(&models.ChatChannel{}).Where("id = ?", ch.ID).UpdateColumn("last_seen", time.Now().UTC())
	text := strings.TrimSpace(in.Text)
	cmd, arg, _ := strings.Cut(text, " ")
	arg = strings.TrimSpace(arg)
	if in.Action == "" && strings.EqualFold(cmd, "/link") {
		s.link(ctx, ch, in, arg)
		return
	}
	if in.Action == "" && (strings.EqualFold(cmd, "/help") || strings.EqualFold(cmd, "/start")) {
		s.reply(ctx, ch, in.ChatID, Outgoing{Text: helpText})
		return
	}
	user, err := s.identity(ctx, ch, in)
	if err != nil {
		s.reply(ctx, ch, in.ChatID, Outgoing{Text: "This chat account is not linked to an Akili user. In Akili, open Chat → Link my account and send /link <code> here."})
		if in.ActionRef != "" {
			s.ack(ctx, ch, in.ActionRef, "Not linked")
		}
		return
	}
	if in.Action != "" {
		s.action(ctx, ch, in, user)
		return
	}
	switch strings.ToLower(cmd) {
	case "/unlink":
		s.db.WithContext(ctx).Where("channel_id = ? AND external_id = ?", ch.ID, in.UserID).Delete(&models.ChatIdentity{})
		s.record(ctx, user, "chat.unlink", ch, nil)
		s.reply(ctx, ch, in.ChatID, Outgoing{Text: "Unlinked."})
	case "/agents":
		s.listAgents(ctx, ch, in, user)
	case "/agent":
		s.selectAgent(ctx, ch, in, user, arg)
	case "/new":
		conv := s.conversation(ctx, ch, in.ChatID)
		s.db.WithContext(ctx).Model(conv).Update("session_id", nil)
		s.reply(ctx, ch, in.ChatID, Outgoing{Text: "New conversation started."})
	case "/status":
		s.status(ctx, ch, in, user)
	case "/task":
		s.task(ctx, ch, in, user, arg)
	case "/approvals":
		s.listApprovals(ctx, ch, in, user)
	case "/approve", "/deny":
		s.decide(ctx, ch, in, user, arg, strings.EqualFold(cmd, "/approve"))
	default:
		if strings.HasPrefix(cmd, "/") {
			s.reply(ctx, ch, in.ChatID, Outgoing{Text: "Unknown command. " + helpText})
			return
		}
		s.message(ctx, ch, in, user, text)
	}
}

func (s *Service) ack(ctx context.Context, ch *models.ChatChannel, ref, text string) {
	p, err := s.PlatformFor(ch)
	if err != nil {
		return
	}
	if a, ok := p.(Acker); ok {
		_ = a.Ack(ctx, ref, text)
	}
}

func (s *Service) record(ctx context.Context, u *models.User, action string, ch *models.ChatChannel, meta map[string]any) {
	if meta == nil {
		meta = map[string]any{}
	}
	meta["channel_id"], meta["channel_kind"] = ch.ID, ch.Kind
	s.audit.Best(ctx, audit.Entry{OrganizationID: ch.OrganizationID, ActorType: audit.ActorUser, ActorID: u.ID, Action: action,
		TargetType: "chat_channel", TargetID: ch.ID, Metadata: meta})
}

// link binds the sender to the Akili user who generated the code. The code proves the Akili side,
// sending it from this account proves the chat side.
func (s *Service) link(ctx context.Context, ch *models.ChatChannel, in Incoming, code string) {
	if !auth.RateLimit(ctx, s.bus.Redis(), "chat-link:"+ch.ID+":"+in.UserID, 5, 10*time.Minute, true) {
		s.reply(ctx, ch, in.ChatID, Outgoing{Text: "Too many link attempts; try again later."})
		return
	}
	var lc models.ChatLinkCode
	err := s.db.WithContext(ctx).First(&lc, "hash = ? AND organization_id = ?", crypto.HashToken(strings.ToUpper(strings.TrimSpace(code))), ch.OrganizationID).Error
	if err != nil || time.Now().After(lc.ExpiresAt) {
		s.reply(ctx, ch, in.ChatID, Outgoing{Text: "That code is invalid or expired. Create a new one in Akili → Chat."})
		return
	}
	// Single use: whoever deletes it first links.
	if res := s.db.WithContext(ctx).Delete(&models.ChatLinkCode{}, "id = ?", lc.ID); res.RowsAffected == 0 {
		s.reply(ctx, ch, in.ChatID, Outgoing{Text: "That code was already used."})
		return
	}
	var u models.User
	if err := s.db.WithContext(ctx).First(&u, "id = ? AND organization_id = ? AND active", lc.UserID, ch.OrganizationID).Error; err != nil {
		s.reply(ctx, ch, in.ChatID, Outgoing{Text: "That Akili user is not active."})
		return
	}
	now := time.Now().UTC()
	var id models.ChatIdentity
	err = s.db.WithContext(ctx).First(&id, "channel_id = ? AND external_id = ?", ch.ID, in.UserID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		id = models.ChatIdentity{Base: models.Base{ID: models.NewID("cid"), OrganizationID: ch.OrganizationID}, ChannelID: ch.ID, ExternalID: in.UserID}
	}
	id.UserID, id.DisplayName, id.LastUsedAt = u.ID, truncate(in.UserName, 190), &now
	if err := s.db.WithContext(ctx).Save(&id).Error; err != nil {
		s.reply(ctx, ch, in.ChatID, Outgoing{Text: "Linking failed; try again."})
		return
	}
	s.record(ctx, &u, "chat.link", ch, map[string]any{"external_id": in.UserID, "display_name": in.UserName})
	s.reply(ctx, ch, in.ChatID, Outgoing{Text: fmt.Sprintf("Linked to %s (%s). Send /help for commands.", u.Email, u.Role)})
}

// identity resolves the sender to an active Akili user.
func (s *Service) identity(ctx context.Context, ch *models.ChatChannel, in Incoming) (*models.User, error) {
	var id models.ChatIdentity
	if err := s.db.WithContext(ctx).First(&id, "channel_id = ? AND external_id = ?", ch.ID, in.UserID).Error; err != nil {
		return nil, err
	}
	var u models.User
	if err := s.db.WithContext(ctx).First(&u, "id = ? AND organization_id = ? AND active", id.UserID, ch.OrganizationID).Error; err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	s.db.WithContext(ctx).Model(&id).Update("last_used_at", now)
	return &u, nil
}

func (s *Service) allowed(ctx context.Context, ch *models.ChatChannel, in Incoming, u *models.User, role string) bool {
	if models.RoleRank(u.Role) >= models.RoleRank(role) {
		return true
	}
	s.reply(ctx, ch, in.ChatID, Outgoing{Text: fmt.Sprintf("Your Akili role (%s) cannot do that; it needs %s.", u.Role, role)})
	return false
}

// conversation returns the chat's conversation row, creating it with the channel's default agent.
func (s *Service) conversation(ctx context.Context, ch *models.ChatChannel, chatID string) *models.ChatConversation {
	var c models.ChatConversation
	if err := s.db.WithContext(ctx).First(&c, "channel_id = ? AND external_id = ?", ch.ID, chatID).Error; err == nil {
		return &c
	}
	c = models.ChatConversation{Base: models.Base{ID: models.NewID("cnv"), OrganizationID: ch.OrganizationID}, ChannelID: ch.ID, ExternalID: chatID,
		AgentID: ch.DefaultAgentID}
	if err := s.db.WithContext(ctx).Create(&c).Error; err != nil {
		// Created concurrently: read the winner.
		s.db.WithContext(ctx).First(&c, "channel_id = ? AND external_id = ?", ch.ID, chatID)
	}
	return &c
}

// agentFor returns the conversation's agent: its own, the channel default, or the only agent.
func (s *Service) agentFor(ctx context.Context, ch *models.ChatChannel, conv *models.ChatConversation) (*models.Agent, error) {
	var a models.Agent
	if conv.AgentID != nil {
		if err := s.db.WithContext(ctx).First(&a, "id = ? AND organization_id = ?", *conv.AgentID, ch.OrganizationID).Error; err == nil {
			return &a, nil
		}
	}
	var agents []models.Agent
	s.db.WithContext(ctx).Where("organization_id = ?", ch.OrganizationID).Limit(2).Find(&agents)
	if len(agents) == 1 {
		return &agents[0], nil
	}
	return nil, errors.New("no agent selected; use /agents and /agent <name>")
}

func (s *Service) listAgents(ctx context.Context, ch *models.ChatChannel, in Incoming, u *models.User) {
	var agents []models.Agent
	s.db.WithContext(ctx).Where("organization_id = ?", ch.OrganizationID).Order("name").Limit(50).Find(&agents)
	if len(agents) == 0 {
		s.reply(ctx, ch, in.ChatID, Outgoing{Text: "No agents yet."})
		return
	}
	var b strings.Builder
	b.WriteString("Agents:\n")
	for _, a := range agents {
		status := "offline"
		if s.bus.Present(ctx, a.ID) {
			status = "online"
		}
		fmt.Fprintf(&b, "- %s (%s)\n", a.Name, status)
	}
	s.reply(ctx, ch, in.ChatID, Outgoing{Text: b.String()})
}

func (s *Service) selectAgent(ctx context.Context, ch *models.ChatChannel, in Incoming, u *models.User, name string) {
	if !s.allowed(ctx, ch, in, u, models.RoleOperator) {
		return
	}
	var a models.Agent
	if err := s.db.WithContext(ctx).First(&a, "organization_id = ? AND lower(name) = ?", ch.OrganizationID, strings.ToLower(name)).Error; err != nil {
		s.reply(ctx, ch, in.ChatID, Outgoing{Text: "No agent named " + name + ". Use /agents."})
		return
	}
	conv := s.conversation(ctx, ch, in.ChatID)
	s.db.WithContext(ctx).Model(conv).Updates(map[string]any{"agent_id": a.ID, "session_id": nil})
	s.reply(ctx, ch, in.ChatID, Outgoing{Text: "Now talking to " + a.Name + "."})
}

func (s *Service) status(ctx context.Context, ch *models.ChatChannel, in Incoming, u *models.User) {
	conv := s.conversation(ctx, ch, in.ChatID)
	var b strings.Builder
	if a, err := s.agentFor(ctx, ch, conv); err == nil {
		state := "offline"
		if s.bus.Present(ctx, a.ID) {
			state = "online"
		}
		fmt.Fprintf(&b, "Agent: %s (%s)\n", a.Name, state)
	} else {
		b.WriteString(err.Error() + "\n")
	}
	var n int64
	s.db.WithContext(ctx).Model(&models.Approval{}).Where("organization_id = ? AND status = ?", ch.OrganizationID, models.ApprovalPending).Count(&n)
	fmt.Fprintf(&b, "Pending approvals: %d\nYou: %s (%s)", n, u.Email, u.Role)
	s.reply(ctx, ch, in.ChatID, Outgoing{Text: b.String()})
}

// message sends text to the conversation's agent session, opening one if needed.
func (s *Service) message(ctx context.Context, ch *models.ChatChannel, in Incoming, u *models.User, text string) {
	if !s.allowed(ctx, ch, in, u, models.RoleOperator) {
		return
	}
	conv := s.conversation(ctx, ch, in.ChatID)
	agent, err := s.agentFor(ctx, ch, conv)
	if err != nil {
		s.reply(ctx, ch, in.ChatID, Outgoing{Text: err.Error()})
		return
	}
	var sess *models.ChatSession
	if conv.SessionID != nil {
		if got, err := s.hub.Get(ctx, ch.OrganizationID, *conv.SessionID); err == nil && got.Status == models.SessionOpen && got.AgentID == agent.ID {
			sess = got
		}
	}
	if sess == nil {
		if sess, err = s.hub.Create(ctx, ch.OrganizationID, agent.ID, u.ID, "Chat ("+ch.Name+")", proto.ModeChat, nil); err != nil {
			s.reply(ctx, ch, in.ChatID, Outgoing{Text: "Could not start a session."})
			return
		}
		s.db.WithContext(ctx).Model(conv).Updates(map[string]any{"session_id": sess.ID, "agent_id": agent.ID})
	}
	if err := s.hub.PostUserMessage(ctx, ch.OrganizationID, sess.ID, u.ID, text); err != nil {
		if errors.Is(err, bus.ErrAgentOffline) {
			s.reply(ctx, ch, in.ChatID, Outgoing{Text: agent.Name + " is offline."})
			return
		}
		s.reply(ctx, ch, in.ChatID, Outgoing{Text: "Could not deliver the message: " + err.Error()})
		return
	}
	s.record(ctx, u, "chat.message", ch, map[string]any{"session_id": sess.ID, "chars": len(text)})
}

func (s *Service) task(ctx context.Context, ch *models.ChatChannel, in Incoming, u *models.User, goal string) {
	if !s.allowed(ctx, ch, in, u, models.RoleOperator) {
		return
	}
	if goal == "" {
		s.reply(ctx, ch, in.ChatID, Outgoing{Text: "Usage: /task <goal>"})
		return
	}
	conv := s.conversation(ctx, ch, in.ChatID)
	agent, err := s.agentFor(ctx, ch, conv)
	if err != nil {
		s.reply(ctx, ch, in.ChatID, Outgoing{Text: err.Error()})
		return
	}
	title := truncate(strings.SplitN(goal, "\n", 2)[0], 120)
	t, err := s.tasks.Create(ctx, ch.OrganizationID, u.ID, tasks.Input{Title: title, Goal: goal, AgentID: &agent.ID,
		Autonomy: tasks.DefaultAutonomy(nil), Trigger: Trigger, TriggerRef: conv.ID})
	if err != nil {
		s.reply(ctx, ch, in.ChatID, Outgoing{Text: "Could not create the task: " + err.Error()})
		return
	}
	s.record(ctx, u, "chat.task", ch, map[string]any{"task_id": t.ID})
	s.reply(ctx, ch, in.ChatID, Outgoing{Text: fmt.Sprintf("Task %s queued on %s. I will post the result here.", t.ID, agent.Name)})
}

func approvalButtons(id string) []Button {
	return []Button{{Label: "Approve", Data: "ap:" + id + ":approve"}, {Label: "Deny", Data: "ap:" + id + ":deny"}}
}

func parseApprovalAction(data string) (id, verb string, ok bool) {
	parts := strings.Split(data, ":")
	if len(parts) != 3 || parts[0] != "ap" || (parts[2] != "approve" && parts[2] != "deny") {
		return "", "", false
	}
	return parts[1], parts[2], true
}

func describeApproval(a *models.Approval) string {
	in := string(a.Input)
	if len(in) > 600 {
		in = in[:600] + "…"
	}
	return fmt.Sprintf("Approval needed (%s risk): %s\n%s\n%s\nid: %s", a.Risk, a.Tool, in, a.Reason, a.ID)
}

func (s *Service) listApprovals(ctx context.Context, ch *models.ChatChannel, in Incoming, u *models.User) {
	var aps []models.Approval
	s.db.WithContext(ctx).Where("organization_id = ? AND status = ?", ch.OrganizationID, models.ApprovalPending).Order("created_at").Limit(10).Find(&aps)
	if len(aps) == 0 {
		s.reply(ctx, ch, in.ChatID, Outgoing{Text: "No pending approvals."})
		return
	}
	for i := range aps {
		s.reply(ctx, ch, in.ChatID, Outgoing{Text: describeApproval(&aps[i]), Buttons: approvalButtons(aps[i].ID)})
	}
}

// action handles a button press.
func (s *Service) action(ctx context.Context, ch *models.ChatChannel, in Incoming, u *models.User) {
	id, verb, ok := parseApprovalAction(in.Action)
	if !ok {
		s.ack(ctx, ch, in.ActionRef, "Unknown action")
		return
	}
	s.decide(ctx, ch, in, u, id, verb == "approve")
}

// decide approves or denies as the linked user, with the same role and audit as the UI.
func (s *Service) decide(ctx context.Context, ch *models.ChatChannel, in Incoming, u *models.User, id string, allow bool) {
	ack := func(text string) {
		if in.ActionRef != "" {
			s.ack(ctx, ch, in.ActionRef, text)
		}
	}
	if models.RoleRank(u.Role) < models.RoleRank(models.RoleOperator) {
		ack("Not allowed")
		s.allowed(ctx, ch, in, u, models.RoleOperator)
		return
	}
	if id == "" {
		s.reply(ctx, ch, in.ChatID, Outgoing{Text: "Usage: /approve <id> or /deny <id>"})
		return
	}
	ap, err := s.hub.Decide(ctx, ch.OrganizationID, id, u.ID, allow, "via "+ch.Kind)
	switch {
	case errors.Is(err, sessions.ErrNotFound):
		ack("Not found")
		s.reply(ctx, ch, in.ChatID, Outgoing{Text: "No approval " + id + "."})
	case errors.Is(err, sessions.ErrApprovalClosed):
		ack("Already decided")
		s.reply(ctx, ch, in.ChatID, Outgoing{Text: "Approval " + id + " was already decided or expired."})
	case err != nil:
		ack("Failed")
		s.reply(ctx, ch, in.ChatID, Outgoing{Text: "Could not decide: " + err.Error()})
	default:
		ack(ap.Status)
		s.record(ctx, u, "chat.approval", ch, map[string]any{"approval_id": ap.ID, "status": ap.Status})
	}
}

// Run polls the polling channels and relays agent replies to chats, on the leader only.
func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	var mu sync.Mutex
	pollers := map[string]context.CancelFunc{} // channel id → poller
	relays := map[string]context.CancelFunc{}  // org id → relay
	stopAll := func() {
		mu.Lock()
		defer mu.Unlock()
		for k, c := range pollers {
			c()
			delete(pollers, k)
		}
		for k, c := range relays {
			c()
			delete(relays, k)
		}
	}
	defer stopAll()
	for {
		if !s.leader.Leading() {
			stopAll()
		} else {
			var chans []models.ChatChannel
			s.db.WithContext(ctx).Where("enabled").Find(&chans)
			want := map[string]bool{}
			orgs := map[string]bool{}
			mu.Lock()
			for i := range chans {
				ch := chans[i]
				orgs[ch.OrganizationID] = true
				if ch.Kind == models.ChatSlack {
					continue // webhooks, no poller
				}
				key := ch.ID + ":" + ch.UpdatedAt.String()
				want[key] = true
				if _, ok := pollers[key]; !ok {
					pctx, cancel := context.WithCancel(ctx)
					pollers[key] = cancel
					go s.poll(pctx, ch)
				}
			}
			for k, c := range pollers {
				if !want[k] {
					c() // channel removed, disabled or edited
					delete(pollers, k)
				}
			}
			for org := range orgs {
				if _, ok := relays[org]; !ok {
					rctx, cancel := context.WithCancel(ctx)
					relays[org] = cancel
					go s.relay(rctx, org)
				}
			}
			mu.Unlock()
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Service) poll(ctx context.Context, ch models.ChatChannel) {
	p, err := s.PlatformFor(&ch)
	if err != nil {
		return
	}
	poller, ok := p.(Poller)
	if !ok {
		return
	}
	backoff := time.Second
	for ctx.Err() == nil {
		msgs, cursor, err := poller.Poll(ctx, ch.Cursor)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			s.db.WithContext(ctx).Model(&models.ChatChannel{}).Where("id = ?", ch.ID).UpdateColumn("last_error", truncate(err.Error(), 480))
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, time.Minute)
			continue
		}
		backoff = time.Second
		if cursor != ch.Cursor {
			ch.Cursor = cursor
			// Saved before handling: a crash skips at most this batch rather than replaying it.
			s.db.WithContext(ctx).Model(&models.ChatChannel{}).Where("id = ?", ch.ID).UpdateColumn("cursor", cursor)
		}
		for _, m := range msgs {
			s.Handle(ctx, &ch, m)
		}
	}
}

// relay forwards session replies, approval requests and chat-task results to their chats.
func (s *Service) relay(ctx context.Context, org string) {
	sub := s.bus.SubscribeOrg(ctx, org)
	defer sub.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-sub.C:
			if !ok {
				return
			}
			s.forward(ctx, ev)
		}
	}
}

// target finds the chat a session belongs to: a chat conversation, or a task started from chat.
func (s *Service) target(ctx context.Context, sessionID string) (*models.ChatChannel, *models.ChatConversation) {
	if sessionID == "" {
		return nil, nil
	}
	var conv models.ChatConversation
	if err := s.db.WithContext(ctx).First(&conv, "session_id = ?", sessionID).Error; err != nil {
		var t models.Task
		if s.db.WithContext(ctx).Select("trigger", "trigger_ref").First(&t, "session_id = ?", sessionID).Error != nil || t.Trigger != Trigger {
			return nil, nil
		}
		if s.db.WithContext(ctx).First(&conv, "id = ?", t.TriggerRef).Error != nil {
			return nil, nil
		}
	}
	var ch models.ChatChannel
	if s.db.WithContext(ctx).First(&ch, "id = ? AND enabled", conv.ChannelID).Error != nil {
		return nil, nil
	}
	return &ch, &conv
}

func (s *Service) forward(ctx context.Context, ev bus.Event) {
	switch ev.Type {
	case sessions.EvMessage:
		var m models.SessionMessage
		if json.Unmarshal(ev.Data, &m) != nil || m.Role != proto.RoleAssistant {
			return
		}
		msg := proto.Message{Role: m.Role, Content: m.Content}
		// Only the final answer of a turn: messages that call tools are intermediate.
		if len(msg.ToolUses()) > 0 || strings.TrimSpace(msg.Text()) == "" {
			return
		}
		// A task's result is posted once, from task.updated.
		var t models.Task
		if s.db.WithContext(ctx).Select("id").First(&t, "session_id = ?", ev.SessionID).Error == nil {
			return
		}
		if ch, conv := s.target(ctx, ev.SessionID); ch != nil {
			s.reply(ctx, ch, conv.ExternalID, Outgoing{Text: msg.Text()})
		}
	case sessions.EvApprovalCreated:
		var a models.Approval
		if json.Unmarshal(ev.Data, &a) != nil {
			return
		}
		if ch, conv := s.target(ctx, ev.SessionID); ch != nil {
			s.reply(ctx, ch, conv.ExternalID, Outgoing{Text: describeApproval(&a), Buttons: approvalButtons(a.ID)})
		}
	case sessions.EvApprovalResolved:
		var a models.Approval
		if json.Unmarshal(ev.Data, &a) != nil || a.Status == models.ApprovalPending {
			return
		}
		if ch, conv := s.target(ctx, ev.SessionID); ch != nil {
			s.reply(ctx, ch, conv.ExternalID, Outgoing{Text: fmt.Sprintf("%s: %s (%s)", a.Tool, a.Status, a.ID)})
		}
	case sessions.EvError:
		var e proto.Error
		if json.Unmarshal(ev.Data, &e) != nil {
			return
		}
		if ch, conv := s.target(ctx, ev.SessionID); ch != nil {
			s.reply(ctx, ch, conv.ExternalID, Outgoing{Text: "Error: " + e.Message})
		}
	case tasks.EvTaskUpdated:
		var t models.Task
		if json.Unmarshal(ev.Data, &t) != nil || t.Trigger != Trigger || !models.TaskTerminal(t.Status) {
			return
		}
		var conv models.ChatConversation
		if s.db.WithContext(ctx).First(&conv, "id = ?", t.TriggerRef).Error != nil {
			return
		}
		var ch models.ChatChannel
		if s.db.WithContext(ctx).First(&ch, "id = ? AND enabled", conv.ChannelID).Error != nil {
			return
		}
		body := t.Result
		if t.Error != "" {
			body = strings.TrimSpace(body + "\n" + t.Error)
		}
		s.reply(ctx, &ch, conv.ExternalID, Outgoing{Text: fmt.Sprintf("Task %s %s: %s\n\n%s", t.ID, t.Status, t.Title, truncate(body, 3000))})
	}
}
