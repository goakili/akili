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

package lessons

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/goakili/akili/server/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRecallFitsTheSession(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Lesson{}); err != nil {
		t.Fatal(err)
	}
	prj := "prj_1"
	base := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	add := func(id, text string, l models.Lesson) {
		l.Base = models.Base{ID: id, OrganizationID: "org_1"}
		l.Text, l.Status = text, models.LessonApproved
		at := base.Add(time.Duration(len(id)) * time.Hour)
		l.DecidedAt = &at
		db.Create(&l)
	}
	add("general", "Use journalctl -u for service logs.", models.Lesson{})
	add("pm-api", "The api runs out of file handles under load; raising LimitNOFILE fixed it.", models.Lesson{Kind: "postmortem", App: "prod/api", Alert: "HighErrorRate"})
	add("pm-web", "The web app needs a cache flush after deploys.", models.Lesson{Kind: "postmortem", App: "prod/web"})
	add("db-host", "This host's disk fills from WAL archives.", models.Lesson{Host: "db"})
	add("other-project", "Run make lint before pushing.", models.Lesson{ProjectID: &prj})

	s := &Service{db: db}
	got := func(q models.LessonQuery) string {
		var ids []string
		for _, l := range s.ForPrompt(context.Background(), "org_1", q) {
			ids = append(ids, l.ID)
		}
		return strings.Join(ids, ",")
	}
	if g := got(models.LessonQuery{AgentID: "ag_1", Text: "HighErrorRate on prod/api"}); g != "pm-api,general" {
		t.Fatalf("api incident recalled %q", g)
	}
	if g := got(models.LessonQuery{AgentID: "ag_1", Text: "SlowRequests on prod/api"}); g != "pm-api,general" {
		t.Fatalf("another alert on the same app recalled %q", g)
	}
	if g := got(models.LessonQuery{AgentID: "ag_1", Labels: []string{"db"}, Text: "disk full"}); g != "db-host,general" {
		t.Fatalf("db host recalled %q", g)
	}
	if g := got(models.LessonQuery{AgentID: "ag_1", ProjectID: "prj_1", Text: "add an endpoint"}); g != "other-project,general" {
		t.Fatalf("project task recalled %q", g)
	}
	for _, l := range s.ForPrompt(context.Background(), "org_1", models.LessonQuery{Text: "prod/api"}) {
		if l.ID == "pm-api" && !strings.HasPrefix(l.Text, "Postmortem (prod/api, HighErrorRate): ") {
			t.Fatalf("postmortem line = %q", l.Text)
		}
	}
}
