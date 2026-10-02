// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package notify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/mail"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/goakili/akili/server/internal/audit"
	"github.com/goakili/akili/server/internal/crypto"
	"github.com/goakili/akili/server/internal/models"
	posta "github.com/goposta/posta-go"
	"github.com/jkaninda/logger"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// Email events.
const (
	EmailApproval = "approval"
	EmailTask     = "task"
	EmailTest     = "test"
)

// approvalEvery limits approval email per person: a burst of requests sends one, and the email
// points at the approvals page, which lists them all.
const approvalEvery = time.Minute

const sendTimeout = 30 * time.Second

// ErrNoPosta means the organization has no default Posta integration.
var ErrNoPosta = errors.New("no Posta integration is set up for email")

// Mailer sends notification email through the organization's default Posta integration. Email
// carries what happened and a link, never tool arguments or output: Posta stores and analyzes what
// it sends, and the audit log in Akili stays the record.
type Mailer struct {
	db        *gorm.DB
	decrypt   func(string) (string, error)
	audit     *audit.Logger
	rdb       *redis.Client
	publicURL string

	mu      sync.Mutex
	clients map[string]cachedClient // by integration id
}

type cachedClient struct {
	updated time.Time
	c       *posta.Client
}

// NewMailer returns a mailer. rdb may be nil (no cross-replica rate limit).
func NewMailer(db *gorm.DB, box *crypto.Box, a *audit.Logger, rdb *redis.Client, publicURL string) *Mailer {
	return &Mailer{db: db, decrypt: box.Decrypt, audit: a, rdb: rdb, publicURL: publicURL, clients: map[string]cachedClient{}}
}

// Message is one notification email.
type Message struct {
	Subject string
	Heading string
	Rows    [][2]string // label, value
	Link    string      // path in the UI
	Action  string      // link text
}

// ApprovalRequested emails the people who can approve and want approval email.
func (m *Mailer) ApprovalRequested(org string, ap *models.Approval, agentName string) {
	if m == nil {
		return
	}
	go m.safely(func(ctx context.Context) {
		var users []models.User
		m.db.WithContext(ctx).Where("organization_id = ? AND active AND email_approvals AND role IN ?", org,
			[]string{models.RoleOperator, models.RoleAdmin, models.RoleOwner}).Find(&users)
		msg := Message{
			Subject: fmt.Sprintf("Approval needed: %s on %s", ap.Tool, agentName),
			Heading: "An agent is waiting for your approval",
			Rows:    [][2]string{{"Agent", agentName}, {"Tool", ap.Tool}, {"Risk", ap.Risk}},
			Link:    "/approvals", Action: "Review in Akili",
		}
		if ap.TaskID != nil {
			msg.Rows = append(msg.Rows, [2]string{"Task", m.taskTitle(ctx, *ap.TaskID)})
		}
		for i := range users {
			if !m.allow(ctx, EmailApproval, users[i].ID, approvalEvery) {
				continue
			}
			_, _ = m.send(ctx, org, &users[i], EmailApproval, msg)
		}
	})
}

// TaskFinished emails the person who started a task, if they want task email.
func (m *Mailer) TaskFinished(org string, t *models.Task) {
	if m == nil || t.CreatedBy == "" {
		return
	}
	go m.safely(func(ctx context.Context) {
		var u models.User
		if err := m.db.WithContext(ctx).First(&u, "id = ? AND organization_id = ? AND active AND email_tasks", t.CreatedBy, org).Error; err != nil {
			return
		}
		agentName := ""
		if t.AssignedAgentID != nil {
			var a models.Agent
			if m.db.WithContext(ctx).Select("name").First(&a, "id = ?", *t.AssignedAgentID).Error == nil {
				agentName = a.Name
			}
		}
		status := strings.ReplaceAll(t.Status, "_", " ")
		rows := [][2]string{{"Status", status}}
		if agentName != "" {
			rows = append(rows, [2]string{"Agent", agentName})
		}
		if t.StartedAt != nil && t.FinishedAt != nil {
			rows = append(rows, [2]string{"Duration", t.FinishedAt.Sub(*t.StartedAt).Round(time.Second).String()})
		}
		rows = append(rows, [2]string{"Cost", fmt.Sprintf("$%.4f", t.CostUSD)})
		_, _ = m.send(ctx, org, &u, EmailTask, Message{
			Subject: fmt.Sprintf("Task %s: %s", status, t.Title),
			Heading: "Your task " + status,
			Rows:    append([][2]string{{"Task", t.Title}}, rows...),
			Link:    "/tasks/" + t.ID, Action: "Open the task",
		})
	})
}

// SendTest emails u now, so they can check delivery. It returns Posta's message id.
func (m *Mailer) SendTest(ctx context.Context, org string, u *models.User) (string, error) {
	return m.send(ctx, org, u, EmailTest, Message{
		Subject: "Akili test email",
		Heading: "Email notifications work",
		Rows:    [][2]string{{"Account", u.Email}},
		Link:    "/settings?tab=account", Action: "Notification settings",
	})
}

// Test validates an integration with a dry-run send: the key, and whether Posta accepts the sender.
func (m *Mailer) Test(ctx context.Context, it *models.Integration) (string, error) {
	c, err := m.client(it)
	if err != nil {
		return "", err
	}
	addr, _ := mail.ParseAddress(it.Sender)
	if addr == nil {
		return "", errors.New("the sender address is invalid")
	}
	if _, err := c.WithContext(ctx).Emails.SendDryRun(&posta.SendEmailRequest{From: it.Sender, To: []string{addr.Address},
		Subject: "Akili connection test", Text: "Dry run: nothing is delivered."}); err != nil {
		return "", err
	}
	return "Posta, sending as " + it.Sender, nil
}

// Available reports whether the organization can send email.
func (m *Mailer) Available(ctx context.Context, org string) bool {
	if m == nil {
		return false
	}
	_, err := m.integration(ctx, org)
	return err == nil
}

func (m *Mailer) send(ctx context.Context, org string, u *models.User, event string, msg Message) (string, error) {
	it, err := m.integration(ctx, org)
	if err != nil {
		return "", err
	}
	c, err := m.client(it)
	if err != nil {
		return "", err
	}
	text, html, err := m.render(msg)
	if err != nil {
		return "", err
	}
	res, err := c.WithContext(ctx).Emails.Send(&posta.SendEmailRequest{From: it.Sender, To: []string{u.Email},
		Subject: oneLine(msg.Subject, 150), Text: text, HTML: html})
	meta := map[string]any{"event": event, "integration_id": it.ID}
	if err != nil {
		meta["error"] = err.Error()
		logger.Warn("notification email failed", "event", event, "user", u.ID, "error", err)
	} else {
		meta["posta_id"] = res.ID
	}
	m.audit.Best(ctx, audit.Entry{OrganizationID: org, ActorType: audit.ActorSystem, Action: "notify.email",
		TargetType: "user", TargetID: u.ID, Metadata: meta})
	if err != nil {
		return "", err
	}
	return res.ID, nil
}

func (m *Mailer) integration(ctx context.Context, org string) (*models.Integration, error) {
	var it models.Integration
	if err := m.db.WithContext(ctx).First(&it, "organization_id = ? AND kind = ? AND is_default", org, models.KindPosta).Error; err != nil {
		return nil, ErrNoPosta
	}
	return &it, nil
}

func (m *Mailer) client(it *models.Integration) (*posta.Client, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cc, ok := m.clients[it.ID]; ok && cc.updated.Equal(it.UpdatedAt) {
		return cc.c, nil
	}
	key, err := m.decrypt(it.TokenEnc)
	if err != nil {
		return nil, err
	}
	tlsCfg, err := crypto.TLSConfigWithCA(it.CACert)
	if err != nil {
		return nil, err
	}
	hc := &http.Client{Timeout: sendTimeout, Transport: &http.Transport{TLSClientConfig: tlsCfg, Proxy: http.ProxyFromEnvironment}}
	c := posta.New(strings.TrimRight(it.BaseURL, "/"), key, posta.WithHTTPClient(hc), posta.WithUserAgent("akili"))
	m.clients[it.ID] = cachedClient{updated: it.UpdatedAt, c: c}
	return c, nil
}

// allow is a per-user rate limit shared by replicas; without Redis it allows.
func (m *Mailer) allow(ctx context.Context, event, userID string, every time.Duration) bool {
	if m.rdb == nil {
		return true
	}
	ok, err := m.rdb.SetNX(ctx, "akili:mail:"+event+":"+userID, 1, every).Result()
	return ok || err != nil
}

func (m *Mailer) taskTitle(ctx context.Context, id string) string {
	var t models.Task
	if err := m.db.WithContext(ctx).Select("title").First(&t, "id = ?", id).Error; err != nil {
		return id
	}
	return t.Title
}

func (m *Mailer) safely(fn func(ctx context.Context)) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*sendTimeout)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			logger.Error("notification email panicked", "panic", r)
		}
	}()
	fn(ctx)
}

var htmlTmpl = template.Must(template.New("mail").Parse(`<!doctype html>
<html><body style="margin:0;padding:24px;background:#f6f6f7;font-family:-apple-system,Segoe UI,Helvetica,Arial,sans-serif;color:#1f2328">
<table role="presentation" style="max-width:520px;margin:0 auto;background:#fff;border:1px solid #e4e4e7;border-radius:10px;padding:24px">
<tr><td>
<p style="margin:0 0 4px;font-size:12px;letter-spacing:.04em;text-transform:uppercase;color:#71717a">Akili</p>
<h1 style="margin:0 0 16px;font-size:18px">{{.Heading}}</h1>
<table role="presentation" style="border-collapse:collapse;font-size:14px;margin-bottom:20px">
{{range .Rows}}<tr><td style="padding:4px 16px 4px 0;color:#71717a">{{index . 0}}</td><td style="padding:4px 0">{{index . 1}}</td></tr>
{{end}}</table>
<a href="{{.URL}}" style="display:inline-block;background:#c2410c;color:#fff;text-decoration:none;padding:9px 16px;border-radius:6px;font-size:14px">{{.Action}}</a>
<p style="margin:20px 0 0;font-size:12px;color:#71717a">Details stay in Akili; open the link to see them. Change which email you get in your notification settings.</p>
</td></tr></table>
</body></html>`))

func (m *Mailer) render(msg Message) (text, html string, err error) {
	rows := make([][2]string, len(msg.Rows))
	var b strings.Builder
	b.WriteString(msg.Heading + "\n\n")
	for i, r := range msg.Rows {
		rows[i] = [2]string{r[0], oneLine(r[1], 200)}
		fmt.Fprintf(&b, "%s: %s\n", r[0], rows[i][1])
	}
	url := strings.TrimRight(m.publicURL, "/") + msg.Link
	fmt.Fprintf(&b, "\n%s: %s\n", msg.Action, url)
	var h bytes.Buffer
	err = htmlTmpl.Execute(&h, map[string]any{"Heading": msg.Heading, "Rows": rows, "URL": url, "Action": msg.Action})
	return b.String(), h.String(), err
}

// oneLine keeps user-provided text (task titles, agent names) to a single bounded line.
func oneLine(s string, max int) string {
	s = strings.Join(strings.FieldsFunc(s, func(r rune) bool { return unicode.IsControl(r) }), " ")
	if r := []rune(s); len(r) > max {
		s = string(r[:max-1]) + "…"
	}
	return strings.TrimSpace(s)
}
