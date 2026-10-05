// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

package pagination

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type row struct {
	ID  uint `gorm:"primaryKey"`
	Org string
}

func TestNewClamps(t *testing.T) {
	for _, c := range []struct{ number, size, wantNumber, wantSize int }{
		{0, 0, 0, DefaultSize},
		{-3, 10, 0, 10},
		{2, MaxSize + 1, 2, MaxSize},
	} {
		if p := New(c.number, c.size); p.Number != c.wantNumber || p.Size != c.wantSize {
			t.Errorf("New(%d, %d) = %+v", c.number, c.size, p)
		}
	}
}

func TestFindPagesThroughEveryRowOnce(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&row{}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 7; i++ {
		db.Create(&row{Org: "a"})
	}
	db.Create(&row{Org: "b"})

	q := db.Where("org = ?", "a")
	seen := map[uint]bool{}
	for n := 0; ; n++ {
		p := New(n, 3)
		out, total, err := Find[row](q, p, "id DESC")
		if err != nil {
			t.Fatal(err)
		}
		if total != 7 {
			t.Fatalf("total = %d, want 7 (the filter must apply to the count)", total)
		}
		for _, r := range out {
			if seen[r.ID] {
				t.Fatalf("row %d returned twice", r.ID)
			}
			seen[r.ID] = true
		}
		if !p.HasNext(total) {
			if n != 2 || len(out) != 1 || p.TotalPages(total) != 3 {
				t.Fatalf("last page %d had %d rows of %d pages", n, len(out), p.TotalPages(total))
			}
			break
		}
	}
	if len(seen) != 7 {
		t.Fatalf("saw %d rows, want 7", len(seen))
	}
	if out, _, _ := Find[row](q, New(9, 3), "id DESC"); len(out) != 0 {
		t.Fatalf("a page past the end returned %d rows", len(out))
	}
}
