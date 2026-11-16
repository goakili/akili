// Copyright (c) 2026 Jonas Kaninda.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
