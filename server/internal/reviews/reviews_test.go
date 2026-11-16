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
