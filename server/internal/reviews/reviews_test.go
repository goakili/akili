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

package reviews

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/goakili/akili/proto"
	"github.com/goakili/akili/server/internal/audit"
	"github.com/goakili/akili/server/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Task{}, &models.Review{}, &models.AuditLog{}); err != nil {
		t.Fatal(err)
	}
	return &Service{db: db, audit: audit.New(db)}, db
}

func seed(t *testing.T, db *gorm.DB, kind string) (*models.ChatSession, string) {
	t.Helper()
	rv := models.Review{Base: models.Base{ID: "rev_1", OrganizationID: "org_1"}, TargetType: models.ReviewTargetChange, TargetID: "chg_1", Status: models.ReviewPending}
	db.Create(&rv)
	task := models.Task{Base: models.Base{ID: "tsk_1", OrganizationID: "org_1"}, Title: "t", Goal: "g", Status: models.TaskRunning, Kind: kind, ReviewID: &rv.ID}
	db.Create(&task)
	return &models.ChatSession{Base: models.Base{ID: "ses_1", OrganizationID: "org_1"}, AgentID: "ag_2", TaskID: &task.ID}, rv.ID
}

func submit(v proto.ReviewSubmitInput) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func TestSubmitOnlyFromAReviewTask(t *testing.T) {
	s, db := newService(t)
	sess, _ := seed(t, db, "")
	res := s.RunRemote(context.Background(), sess, proto.ToolReviewSubmit, submit(proto.ReviewSubmitInput{Verdict: proto.VerdictLooksGood, Summary: "fine"}))
	if !res.IsError {
		t.Fatalf("an ordinary task submitted a review: %+v", res)
	}
}

func TestSubmitOnce(t *testing.T) {
	s, db := newService(t)
	sess, id := seed(t, db, models.TaskKindReview)
	in := submit(proto.ReviewSubmitInput{Verdict: proto.VerdictConcerns, Summary: "rollback misses the config file", Concerns: []string{"no rollback for /etc/app.conf", " "}})
	if res := s.RunRemote(context.Background(), sess, proto.ToolReviewSubmit, in); res.IsError {
		t.Fatalf("submit: %+v", res)
	}
	var rv models.Review
	db.First(&rv, "id = ?", id)
	if rv.Status != models.ReviewDone || rv.Verdict != proto.VerdictConcerns || len(rv.Concerns) != 1 || *rv.ReviewerAgentID != "ag_2" {
		t.Fatalf("review = %+v", rv)
	}
	if res := s.RunRemote(context.Background(), sess, proto.ToolReviewSubmit, in); !res.IsError || !strings.Contains(res.Output, "already") {
		t.Fatalf("a second submission was accepted: %+v", res)
	}
}

func TestReviewerStoppingWithoutSubmitting(t *testing.T) {
	s, db := newService(t)
	_, id := seed(t, db, models.TaskKindReview)
	s.taskFinished(context.Background(), &models.Task{Kind: models.TaskKindReview, ReviewID: &id, Status: models.TaskSucceeded, Result: "Looks fine to me."})
	var rv models.Review
	db.First(&rv, "id = ?", id)
	if rv.Status != models.ReviewDone || rv.Verdict != "" || rv.Summary != "Looks fine to me." {
		t.Fatalf("review = %+v", rv)
	}
}
