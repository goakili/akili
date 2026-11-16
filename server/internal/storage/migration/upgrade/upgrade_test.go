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

package upgrade

import (
	"context"
	"errors"
	"testing"

	"github.com/goakili/akili/server/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type note struct {
	ID   uint `gorm:"primaryKey"`
	Text string
}

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.UpgradeStep{}, &note{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func write(text string) func(context.Context, *gorm.DB) error {
	return func(_ context.Context, tx *gorm.DB) error { return tx.Create(&note{Text: text}).Error }
}

func notes(t *testing.T, db *gorm.DB) []string {
	t.Helper()
	var rows []note
	if err := db.Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Text
	}
	return out
}

func TestStepsRunOnceInOrder(t *testing.T) {
	db := testDB(t)
	list := []Step{{Name: "first", Version: "0.0.1", Run: write("a")}, {Name: "second", Version: "0.0.2", Run: write("b")}}
	for range 2 {
		if err := run(context.Background(), db, list); err != nil {
			t.Fatal(err)
		}
	}
	if got := notes(t, db); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("steps ran %v, want [a b] exactly once", got)
	}
	var rec models.UpgradeStep
	if err := db.First(&rec, "name = ?", "second").Error; err != nil || rec.Version != "0.0.2" || rec.AppliedAt.IsZero() {
		t.Fatalf("record %+v, %v", rec, err)
	}
}

func TestFailedStepLeavesNothingAndRunsAgain(t *testing.T) {
	db := testDB(t)
	broken := Step{Name: "broken", Version: "0.0.1", Run: func(ctx context.Context, tx *gorm.DB) error {
		if err := write("half done")(ctx, tx); err != nil {
			return err
		}
		return errors.New("boom")
	}}
	after := Step{Name: "after", Version: "0.0.1", Run: write("after")}
	if err := run(context.Background(), db, []Step{broken, after}); err == nil {
		t.Fatal("a failing step reported success")
	}
	var n int64
	db.Model(&models.UpgradeStep{}).Count(&n)
	if got := notes(t, db); len(got) != 0 || n != 0 {
		t.Fatalf("after a failure: notes %v, records %d; want none", got, n)
	}

	fixed := Step{Name: "broken", Version: "0.0.1", Run: write("fixed")}
	if err := run(context.Background(), db, []Step{fixed, after}); err != nil {
		t.Fatal(err)
	}
	if got := notes(t, db); len(got) != 2 || got[0] != "fixed" || got[1] != "after" {
		t.Fatalf("after the fix: %v", got)
	}
}

func TestStepNamesMustBeUnique(t *testing.T) {
	db := testDB(t)
	for name, list := range map[string][]Step{
		"duplicate": {{Name: "same", Run: write("1")}, {Name: "same", Run: write("2")}},
		"empty":     {{Name: "", Run: write("1")}},
	} {
		if err := run(context.Background(), db, list); err == nil {
			t.Errorf("%s names were accepted", name)
		}
	}
	if got := notes(t, db); len(got) != 0 {
		t.Fatalf("a step ran before the registry was checked: %v", got)
	}
}

func TestRegisteredStepsAreValid(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range steps {
		if s.Name == "" || s.Version == "" || s.Run == nil || seen[s.Name] {
			t.Errorf("step %+v: needs a unique name, a version and a Run", s)
		}
		seen[s.Name] = true
	}
}
