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

package handlers

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/middlewares"
	"github.com/goakili/akili/server/internal/models"
	"github.com/goakili/akili/server/internal/sessions"
	"github.com/goakili/akili/server/internal/storage/pagination"
	"github.com/jkaninda/okapi"
)

func (h *Handlers) ListSessions(c *okapi.Context) error {
	q := h.DB.Where("organization_id = ?", middlewares.OrgID(c))
	if a := c.Query("agent_id"); a != "" {
		q = q.Where("agent_id = ?", a)
	}
	if t := c.Query("task_id"); t != "" {
		q = q.Where("task_id = ?", t)
	}
	if m := c.Query("mode"); m != "" {
		q = q.Where("mode = ?", m)
	}
	if pid := c.Query("project_id"); pid != "" {
		q = q.Where("project_id = ?", pid)
	}
	p := pageParams(c)
	out, total, err := pagination.Find[models.ChatSession](q, p, "updated_at DESC, id DESC")
	if err != nil {
		return c.AbortInternalServerError("list failed", err)
	}
	return paged(c, out, total, p)
}

type SessionRequest struct {
	Body struct {
		AgentID   string `json:"agent_id" required:"true"`
		Title     string `json:"title"`
		ProjectID string `json:"project_id" description:"work interactively on a project's repository"`
	} `json:"body"`
}

func (h *Handlers) CreateSession(c *okapi.Context, req *SessionRequest) error {
	org := middlewares.OrgID(c)
	a, err := h.Fleet.Get(c.Request().Context(), org, req.Body.AgentID)
	if err != nil {
		return mapErr(c, err)
	}
	if a.Status == models.AgentRevoked || a.Status == models.AgentPending {
		return c.AbortConflict("the agent is not enrolled")
	}
	if req.Body.ProjectID != "" {
		if _, err := h.Coder.Project(c.Request().Context(), org, req.Body.ProjectID); err != nil {
			return c.AbortBadRequest("unknown project")
		}
	}
	s, err := h.Sessions.Create(c.Request().Context(), org, a.ID, middlewares.UserID(c), strings.TrimSpace(req.Body.Title), proto.ModeChat, nil, req.Body.ProjectID)
	if err != nil {
		return c.AbortInternalServerError("create failed", err)
	}
	h.record(c, "session.create", "session", s.ID, map[string]any{"agent_id": a.ID})
	return created(c, s)
}

type SessionDetail struct {
	Session   *models.ChatSession     `json:"session"`
	Messages  []models.SessionMessage `json:"messages"`
	Events    []models.SessionEvent   `json:"events"`
	Approvals []models.Approval       `json:"approvals"`
	Questions []models.Question       `json:"questions"`
	Project   *models.Project         `json:"project,omitempty"`
}

func (h *Handlers) GetSession(c *okapi.Context) error {
	s, err := h.Sessions.Get(c.Request().Context(), middlewares.OrgID(c), c.Param("id"))
	if err != nil {
		return mapErr(c, err)
	}
	d := SessionDetail{Session: s}
	h.DB.Where("session_id = ?", s.ID).Order("id").Find(&d.Messages)
	h.DB.Where("session_id = ?", s.ID).Order("id").Limit(2000).Find(&d.Events)
	h.DB.Where("session_id = ?", s.ID).Order("created_at").Find(&d.Approvals)
	h.DB.Where("session_id = ?", s.ID).Order("created_at").Find(&d.Questions)
	if s.ProjectID != nil {
		d.Project, _ = h.Coder.Project(c.Request().Context(), s.OrganizationID, *s.ProjectID)
	}
	return ok(c, d)
}

// MessageRequest posts operator input: text, images uploaded to the session first, or both.
type MessageRequest struct {
	Body struct {
		Text        string   `json:"text" maxLength:"100000"`
		Attachments []string `json:"attachments" maxItems:"5" description:"Attachment ids from POST /sessions/{id}/attachments"`
	} `json:"body"`
}

func (h *Handlers) PostMessage(c *okapi.Context, req *MessageRequest) error {
	if strings.TrimSpace(req.Body.Text) == "" && len(req.Body.Attachments) == 0 {
		return c.AbortBadRequest("text or an image is required")
	}
	if err := h.Sessions.PostUserMessage(c.Request().Context(), middlewares.OrgID(c), c.Param("id"), middlewares.UserID(c), req.Body.Text,
		req.Body.Attachments...); err != nil {
		return mapErr(c, err)
	}
	return message(c, "sent")
}

// UploadAttachment stores an image for the session. The body is the raw image; the type is sniffed.
func (h *Handlers) UploadAttachment(c *okapi.Context) error {
	data, err := io.ReadAll(http.MaxBytesReader(c.ResponseWriter(), c.Request().Body, proto.MaxImageBytes+1))
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		return c.AbortRequestEntityTooLarge("the image is larger than 5 MB")
	}
	if err != nil {
		return c.AbortBadRequest("could not read the image")
	}
	a, err := h.Sessions.SaveAttachment(c.Request().Context(), middlewares.OrgID(c), c.Param("id"), middlewares.UserID(c), data)
	if errors.Is(err, sessions.ErrNotImage) {
		return c.AbortUnsupportedMediaType(err.Error())
	}
	if err != nil {
		return mapErr(c, err)
	}
	return created(c, a)
}

func (h *Handlers) GetAttachment(c *okapi.Context) error {
	a, err := h.Sessions.Attachment(c.Request().Context(), middlewares.OrgID(c), c.Param("id"), c.Param("attachment"))
	if err != nil {
		return mapErr(c, err)
	}
	c.SetHeader("Cache-Control", "private, max-age=86400, immutable")
	c.SetHeader("X-Content-Type-Options", "nosniff")
	c.SetHeader("Content-Security-Policy", "default-src 'none'; sandbox")
	return c.Data(http.StatusOK, a.MediaType, a.Data)
}

// InterruptSession stops the agent's current turn.
func (h *Handlers) InterruptSession(c *okapi.Context) error {
	if err := h.Sessions.Interrupt(c.Request().Context(), middlewares.OrgID(c), c.Param("id"), middlewares.UserID(c)); err != nil {
		return mapErr(c, err)
	}
	return message(c, "interrupted")
}

func (h *Handlers) CloseSession(c *okapi.Context) error {
	if err := h.Sessions.Close(c.Request().Context(), middlewares.OrgID(c), c.Param("id"), middlewares.UserID(c)); err != nil {
		return mapErr(c, err)
	}
	return message(c, "closed")
}

func (h *Handlers) StreamSession(c *okapi.Context) error {
	s, err := h.Sessions.Get(c.Request().Context(), middlewares.OrgID(c), c.Param("id"))
	if err != nil {
		return mapErr(c, err)
	}
	return streamEvents(c, h.Bus.SubscribeSession(c.Request().Context(), s.ID), nil)
}

// StreamEvents is the one stream a tab needs (browsers allow ~6 HTTP/1.1 connections per host); token deltas only for ?session=.
func (h *Handlers) StreamEvents(c *okapi.Context) error {
	focus := c.Query("session")
	return streamEvents(c, h.Bus.SubscribeOrg(c.Request().Context(), middlewares.OrgID(c)), func(ev busEvent) bool {
		return ev.Type != sessions.EvDelta || (focus != "" && ev.SessionID == focus)
	})
}

func (h *Handlers) ListApprovals(c *okapi.Context) error {
	q := h.DB.Where("organization_id = ?", middlewares.OrgID(c))
	if st := c.Query("status"); st != "" {
		q = q.Where("status = ?", st)
	}
	p := pageParams(c)
	out, total, err := pagination.Find[models.Approval](q, p, "created_at DESC, id DESC")
	if err != nil {
		return c.AbortInternalServerError("list failed", err)
	}
	return paged(c, out, total, p)
}

type DecisionRequest struct {
	Body struct {
		Note string `json:"note" maxLength:"500"`
	} `json:"body"`
}

// Approve allows the exact tool call.
func (h *Handlers) Approve(c *okapi.Context, req *DecisionRequest) error {
	ap, err := h.Sessions.Decide(c.Request().Context(), middlewares.OrgID(c), c.Param("id"), middlewares.UserID(c), true, req.Body.Note)
	if err != nil {
		return mapErr(c, err)
	}
	return ok(c, ap)
}

func (h *Handlers) Deny(c *okapi.Context, req *DecisionRequest) error {
	ap, err := h.Sessions.Decide(c.Request().Context(), middlewares.OrgID(c), c.Param("id"), middlewares.UserID(c), false, req.Body.Note)
	if err != nil {
		return mapErr(c, err)
	}
	return ok(c, ap)
}
