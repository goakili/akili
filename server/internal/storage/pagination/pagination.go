// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package pagination pages list queries by offset (page and size, Miabi style).
package pagination

import "gorm.io/gorm"

// Page sizes: lists return DefaultSize rows unless the client asks for up to MaxSize.
const (
	DefaultSize = 50
	MaxSize     = 200
)

// Page is a 0-based page number and a page size.
type Page struct {
	Number int
	Size   int
}

// New clamps a requested page to valid bounds.
func New(number, size int) Page {
	if number < 0 {
		number = 0
	}
	if size <= 0 {
		size = DefaultSize
	}
	return Page{Number: number, Size: min(size, MaxSize)}
}

// HasNext reports whether rows remain after this page.
func (p Page) HasNext(total int64) bool { return int64(p.Number+1)*int64(p.Size) < total }

// TotalPages is the number of pages total rows fill.
func (p Page) TotalPages(total int64) int { return int((total + int64(p.Size) - 1) / int64(p.Size)) }

// Find counts the rows q matches and loads one page of them. order must end on a unique column so
// rows do not move between pages.
func Find[T any](q *gorm.DB, p Page, order string) ([]T, int64, error) {
	var total int64
	if err := q.Session(&gorm.Session{}).Model(new(T)).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	out := make([]T, 0, min(p.Size, int(max(total, 0))))
	if total == 0 || int64(p.Number)*int64(p.Size) >= total {
		return out, total, nil
	}
	err := q.Session(&gorm.Session{}).Order(order).Offset(p.Number * p.Size).Limit(p.Size).Find(&out).Error
	return out, total, err
}
