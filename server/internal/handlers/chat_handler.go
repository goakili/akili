// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package handlers

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/goakili/akili/server/internal/chat"
	"github.com/goakili/akili/server/internal/lessons"
	"github.com/goakili/akili/server/internal/middlewares"
	"github.com/goakili/akili/server/internal/models"
	"github.com/jkaninda/okapi"
)

// ChatChannelRequest creates or updates a chat channel. Credentials are write-only: empty keeps them.
type ChatChannelRequest struct {
	Body struct {
		Name           string  `json:"name" required:"true"`
		Kind           string  `json:"kind" required:"true" description:"slack, telegram or signal"`
		Enabled        *bool   `json:"enabled"`
		APIBaseURL     string  `json:"api_base_url" description:"signal: the signal-cli REST server URL (required); otherwise empty for the public API"`
		Account        string  `json:"account" description:"signal: the bot's number"`
		Token          string  `json:"token" description:"slack: bot token (xoxb-…); telegram: bot token"`
		SigningSecret  string  `json:"signing_secret" description:"slack: signing secret"`
		DefaultAgentID *string `json:"default_agent_id"`
	} `json:"body"`
}

// ChatChannelView adds the Slack request URLs to a channel.
type ChatChannelView struct {
	models.ChatChannel
	EventsURL   string `json:"events_url,omitempty"`
	InteractURL string `json:"interact_url,omitempty"`
}

func (h *Handlers) channelView(ch models.ChatChannel) ChatChannelView {
	v := ChatChannelView{ChatChannel: ch}
	if ch.Kind == models.ChatSlack {
		base := strings.TrimRight(h.Cfg.PublicURL, "/") + "/api/v1/chat/slack/" + ch.ID
		v.EventsURL, v.InteractURL = base+"/events", base+"/interact"
	}
	return v
}

// ListChatChannels lists chat channels.
func (h *Handlers) ListChatChannels(c *okapi.Context) error {
	var rows []models.ChatChannel
	h.DB.Where("organization_id = ?", middlewares.OrgID(c)).Order("name").Find(&rows)
	out := make([]ChatChannelView, len(rows))
	for i := range rows {
		out[i] = h.channelView(rows[i])
	}
	return ok(c, out)
}

func (h *Handlers) saveChannel(c *okapi.Context, ch *models.ChatChannel, req *ChatChannelRequest, isNew bool) error {
	b := req.Body
	switch b.Kind {
	case models.ChatSlack, models.ChatTelegram, models.ChatSignal:
	default:
		return c.AbortBadRequest("kind must be slack, telegram or signal")
	}
	if !isNew && b.Kind != ch.Kind {
		return c.AbortBadRequest("the kind of a channel cannot change")
	}
	ch.Name, ch.Kind, ch.APIBaseURL, ch.Account = strings.TrimSpace(b.Name), b.Kind, strings.TrimRight(strings.TrimSpace(b.APIBaseURL), "/"), strings.TrimSpace(b.Account)
	if b.Enabled != nil {
		ch.Enabled = *b.Enabled
	}
	ch.DefaultAgentID = emptyNil(b.DefaultAgentID)
	if ch.DefaultAgentID != nil {
		var n int64
		h.DB.Model(&models.Agent{}).Where("id = ? AND organization_id = ?", *ch.DefaultAgentID, ch.OrganizationID).Count(&n)
		if n == 0 {
			return c.AbortBadRequest("unknown default agent")
		}
	}
	var err error
	if b.Token != "" {
		if ch.TokenEnc, err = h.Box.Encrypt(b.Token); err != nil {
			return c.AbortInternalServerError("encryption failed", err)
		}
	}
	if b.SigningSecret != "" {
		if ch.SecretEnc, err = h.Box.Encrypt(b.SigningSecret); err != nil {
			return c.AbortInternalServerError("encryption failed", err)
		}
	}
	switch {
	case ch.Name == "":
		return c.AbortBadRequest("name is required")
	case ch.Kind == models.ChatSlack && (ch.TokenEnc == "" || ch.SecretEnc == ""):
		return c.AbortBadRequest("slack needs the bot token and the signing secret")
	case ch.Kind == models.ChatTelegram && ch.TokenEnc == "":
		return c.AbortBadRequest("telegram needs the bot token")
	case ch.Kind == models.ChatSignal && (ch.APIBaseURL == "" || ch.Account == ""):
		return c.AbortBadRequest("signal needs the signal-cli REST server URL and the account number")
	}
	return h.DB.Save(ch).Error
}

// CreateChatChannel adds a chat channel.
func (h *Handlers) CreateChatChannel(c *okapi.Context, req *ChatChannelRequest) error {
	ch := &models.ChatChannel{Base: models.Base{ID: models.NewID("chn"), OrganizationID: middlewares.OrgID(c)}, Enabled: true, CreatedBy: middlewares.UserID(c)}
	if err := h.saveChannel(c, ch, req, true); err != nil {
		return err
	}
	h.record(c, "chat_channel.create", "chat_channel", ch.ID, map[string]any{"name": ch.Name, "kind": ch.Kind})
	ch.HasToken, ch.HasSecret = ch.TokenEnc != "", ch.SecretEnc != ""
	return created(c, h.channelView(*ch))
}

// UpdateChatChannel edits a chat channel.
func (h *Handlers) UpdateChatChannel(c *okapi.Context, req *ChatChannelRequest) error {
	var ch models.ChatChannel
	if err := h.DB.First(&ch, "id = ? AND organization_id = ?", c.Param("id"), middlewares.OrgID(c)).Error; err != nil {
		return c.AbortNotFound("channel not found")
	}
	if err := h.saveChannel(c, &ch, req, false); err != nil {
		return err
	}
	h.record(c, "chat_channel.update", "chat_channel", ch.ID, map[string]any{"enabled": ch.Enabled})
	return ok(c, h.channelView(ch))
}

// DeleteChatChannel removes a channel with its identities and conversations.
func (h *Handlers) DeleteChatChannel(c *okapi.Context) error {
	id, org := c.Param("id"), middlewares.OrgID(c)
	res := h.DB.Where("id = ? AND organization_id = ?", id, org).Delete(&models.ChatChannel{})
	if res.RowsAffected == 0 {
		return c.AbortNotFound("channel not found")
	}
	h.DB.Where("channel_id = ?", id).Delete(&models.ChatIdentity{})
	h.DB.Where("channel_id = ?", id).Delete(&models.ChatConversation{})
	h.record(c, "chat_channel.delete", "chat_channel", id, nil)
	return message(c, "deleted")
}

// TestChatChannel checks a channel's credentials against the platform.
func (h *Handlers) TestChatChannel(c *okapi.Context) error {
	var ch models.ChatChannel
	if err := h.DB.First(&ch, "id = ? AND organization_id = ?", c.Param("id"), middlewares.OrgID(c)).Error; err != nil {
		return c.AbortNotFound("channel not found")
	}
	start := time.Now()
	who, err := "", error(nil)
	p, err := h.Chat.PlatformFor(&ch)
	if err == nil {
		ctx, cancel := context.WithTimeout(c.Request().Context(), 15*time.Second)
		defer cancel()
		who, err = p.Test(ctx)
	}
	out := TestResult{LatencyMs: time.Since(start).Milliseconds()}
	if err != nil {
		out.Error = err.Error()
	} else {
		out.OK, out.Reply = true, "connected as "+who
	}
	h.record(c, "chat_channel.test", "chat_channel", ch.ID, map[string]any{"ok": out.OK})
	return ok(c, out)
}

// ChatLinkCode is a one-time code to link a chat account.
type ChatLinkCode struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
}

// CreateChatLinkCode issues a link code for the caller.
func (h *Handlers) CreateChatLinkCode(c *okapi.Context) error {
	code, exp, err := h.Chat.NewLinkCode(c.Request().Context(), middlewares.OrgID(c), middlewares.UserID(c))
	if err != nil {
		return c.AbortInternalServerError("could not create a code", err)
	}
	h.record(c, "chat.link_code", "user", middlewares.UserID(c), nil)
	return created(c, ChatLinkCode{Code: code, ExpiresAt: exp})
}

// ListChatIdentities lists linked chat accounts: all for admins, the caller's own otherwise.
func (h *Handlers) ListChatIdentities(c *okapi.Context) error {
	q := h.DB.Where("organization_id = ?", middlewares.OrgID(c))
	if models.RoleRank(middlewares.Role(c)) < models.RoleRank(models.RoleAdmin) {
		q = q.Where("user_id = ?", middlewares.UserID(c))
	}
	var out []models.ChatIdentity
	q.Order("created_at DESC").Find(&out)
	return ok(c, out)
}

// DeleteChatIdentity unlinks a chat account (admins any, users their own).
func (h *Handlers) DeleteChatIdentity(c *okapi.Context) error {
	q := h.DB.Where("id = ? AND organization_id = ?", c.Param("id"), middlewares.OrgID(c))
	if models.RoleRank(middlewares.Role(c)) < models.RoleRank(models.RoleAdmin) {
		q = q.Where("user_id = ?", middlewares.UserID(c))
	}
	if res := q.Delete(&models.ChatIdentity{}); res.RowsAffected == 0 {
		return c.AbortNotFound("identity not found")
	}
	h.record(c, "chat.unlink", "chat_identity", c.Param("id"), nil)
	return message(c, "unlinked")
}

// slackChannel loads an enabled Slack channel and verifies the request signature.
func (h *Handlers) slackChannel(c *okapi.Context) (*models.ChatChannel, []byte, error) {
	var ch models.ChatChannel
	if err := h.DB.First(&ch, "id = ? AND kind = ? AND enabled", c.Param("id"), models.ChatSlack).Error; err != nil {
		return nil, nil, c.AbortNotFound("channel not found")
	}
	body, err := io.ReadAll(io.LimitReader(c.Request().Body, 1<<20))
	if err != nil {
		return nil, nil, c.AbortBadRequest("unreadable body")
	}
	p, err := h.Chat.PlatformFor(&ch)
	if err != nil {
		return nil, nil, c.AbortInternalServerError("channel misconfigured", err)
	}
	if err := p.(*chat.Slack).Verify(c.Request().Header, body, time.Now()); err != nil {
		return nil, nil, c.AbortUnauthorized("invalid signature")
	}
	return &ch, body, nil
}

// SlackEvents receives Slack Events API requests.
func (h *Handlers) SlackEvents(c *okapi.Context) error {
	ch, body, err := h.slackChannel(c)
	if ch == nil {
		return err
	}
	ev, err := chat.ParseEvent(body)
	if err != nil {
		return c.AbortBadRequest("invalid event")
	}
	if ev.Challenge != "" {
		return c.JSON(200, map[string]string{"challenge": ev.Challenge})
	}
	// Slack retries until it gets a 2xx within 3s: handle once, in the background.
	if ev.Message != nil && h.Bus.Redis().SetNX(c.Request().Context(), "akili:slack-event:"+ev.EventID, 1, time.Hour).Val() {
		go h.Chat.Handle(context.Background(), ch, *ev.Message)
	}
	return c.JSON(200, map[string]bool{"ok": true})
}

// SlackInteract receives Slack button presses.
func (h *Handlers) SlackInteract(c *okapi.Context) error {
	ch, body, err := h.slackChannel(c)
	if ch == nil {
		return err
	}
	in, err := chat.ParseAction(body)
	if err != nil {
		return c.AbortBadRequest(err.Error())
	}
	go h.Chat.Handle(context.Background(), ch, *in)
	return c.JSON(200, map[string]bool{"ok": true})
}

// LessonRequest creates an operator lesson.
type LessonRequest struct {
	Body struct {
		Text    string  `json:"text" required:"true"`
		AgentID *string `json:"agent_id" description:"empty: applies to every agent"`
	} `json:"body"`
}

// LessonDecisionRequest approves (optionally editing the text) or rejects a lesson.
type LessonDecisionRequest struct {
	Body struct {
		Text string `json:"text" description:"approve: the final text (default: as proposed)"`
		Note string `json:"note"`
	} `json:"body"`
}

// ListLessons lists lessons.
func (h *Handlers) ListLessons(c *okapi.Context) error {
	out, err := h.Lessons.List(c.Request().Context(), middlewares.OrgID(c), lessons.Filter{Status: c.Query("status"), AgentID: c.Query("agent_id")})
	if err != nil {
		return c.AbortInternalServerError("list failed", err)
	}
	return ok(c, out)
}

// CreateLesson adds an approved lesson written by an operator.
func (h *Handlers) CreateLesson(c *okapi.Context, req *LessonRequest) error {
	agent := emptyNil(req.Body.AgentID)
	if agent != nil {
		var n int64
		h.DB.Model(&models.Agent{}).Where("id = ? AND organization_id = ?", *agent, middlewares.OrgID(c)).Count(&n)
		if n == 0 {
			return c.AbortBadRequest("unknown agent")
		}
	}
	l, err := h.Lessons.Create(c.Request().Context(), middlewares.OrgID(c), middlewares.UserID(c), agent, req.Body.Text)
	if err != nil {
		return lessonErr(c, err)
	}
	return created(c, l)
}

func lessonErr(c *okapi.Context, err error) error {
	switch {
	case errors.Is(err, lessons.ErrNotFound):
		return c.AbortNotFound(err.Error())
	case errors.Is(err, lessons.ErrInvalid):
		return c.AbortBadRequest(err.Error())
	}
	return c.AbortInternalServerError("lesson update failed", err)
}

// ApproveLesson approves a proposed lesson.
func (h *Handlers) ApproveLesson(c *okapi.Context, req *LessonDecisionRequest) error {
	l, err := h.Lessons.Decide(c.Request().Context(), middlewares.OrgID(c), middlewares.UserID(c), c.Param("id"), true, req.Body.Text, req.Body.Note)
	if err != nil {
		return lessonErr(c, err)
	}
	return ok(c, l)
}

// RejectLesson rejects a lesson; it never reaches prompts.
func (h *Handlers) RejectLesson(c *okapi.Context, req *LessonDecisionRequest) error {
	l, err := h.Lessons.Decide(c.Request().Context(), middlewares.OrgID(c), middlewares.UserID(c), c.Param("id"), false, "", req.Body.Note)
	if err != nil {
		return lessonErr(c, err)
	}
	return ok(c, l)
}

// DeleteLesson removes a lesson.
func (h *Handlers) DeleteLesson(c *okapi.Context) error {
	if err := h.Lessons.Delete(c.Request().Context(), middlewares.OrgID(c), middlewares.UserID(c), c.Param("id")); err != nil {
		return lessonErr(c, err)
	}
	return message(c, "deleted")
}
