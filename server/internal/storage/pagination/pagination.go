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

func New(number, size int) Page {
	if number < 0 {
		number = 0
	}
	if size <= 0 {
		size = DefaultSize
	}
	return Page{Number: number, Size: min(size, MaxSize)}
}

func (p Page) HasNext(total int64) bool { return int64(p.Number+1)*int64(p.Size) < total }

func (p Page) TotalPages(total int64) int { return int((total + int64(p.Size) - 1) / int64(p.Size)) }

// Find counts and loads one page; order must end on a unique column so rows do not move between pages.
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
