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

// Package dto holds the API envelopes and the error handler that produces them.
package dto

import (
	"net/http"

	"github.com/jkaninda/okapi"
)

type Response[T any] struct {
	Success bool `json:"success"`
	Data    T    `json:"data"`
}

// PageResponse carries Pageable only when a next page exists, so a client keeps loading until it is absent.
type PageResponse[T any] struct {
	Success  bool      `json:"success"`
	Data     []T       `json:"data"`
	Pageable *Pageable `json:"pageable,omitempty"`
}

type Pageable struct {
	CurrentPage   int   `json:"current_page"`
	NextPage      int   `json:"next_page"`
	Size          int   `json:"size"`
	TotalPages    int   `json:"total_pages"`
	TotalElements int64 `json:"total_elements"`
}

type ErrorInfo struct {
	StatusCode int    `json:"status_code"`
	Code       string `json:"code"`
	Message    string `json:"message"`
}

type ErrorResponse struct {
	Success bool      `json:"success"`
	Error   ErrorInfo `json:"error"`
}

type Message struct {
	Message string `json:"message"`
}

// ErrorHandler renders every error as the envelope; 5xx details are logged, never echoed.
func ErrorHandler() okapi.ErrorHandler {
	return func(c *okapi.Context, code int, message string, err error) error {
		if code < 500 && err != nil && (message == "" || message == http.StatusText(code)) {
			message = err.Error()
		}
		if message == "" {
			message = http.StatusText(code)
		}
		return c.JSON(code, ErrorResponse{Error: ErrorInfo{StatusCode: code, Code: codeFor(code), Message: message}})
	}
}

func codeFor(status int) string {
	switch status {
	case 400:
		return "BAD_REQUEST"
	case 401:
		return "UNAUTHORIZED"
	case 403:
		return "FORBIDDEN"
	case 404:
		return "NOT_FOUND"
	case 409:
		return "CONFLICT"
	case 422:
		return "UNPROCESSABLE_ENTITY"
	case 429:
		return "TOO_MANY_REQUESTS"
	case 503:
		return "SERVICE_UNAVAILABLE"
	}
	if status >= 500 {
		return "INTERNAL_ERROR"
	}
	return "ERROR"
}
