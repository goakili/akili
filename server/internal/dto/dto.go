// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package dto holds the API envelopes and the error handler that produces them.
package dto

import (
	"net/http"

	"github.com/jkaninda/okapi"
)

// Response is the success envelope.
type Response[T any] struct {
	Success bool `json:"success"`
	Data    T    `json:"data"`
}

// PageResponse is one page of a list. Pageable is present only when a next page exists, so a
// client keeps loading until it is absent.
type PageResponse[T any] struct {
	Success  bool      `json:"success"`
	Data     []T       `json:"data"`
	Pageable *Pageable `json:"pageable,omitempty"`
}

// Pageable describes the page just returned and where the next one starts.
type Pageable struct {
	CurrentPage   int   `json:"current_page"`
	NextPage      int   `json:"next_page"`
	Size          int   `json:"size"`
	TotalPages    int   `json:"total_pages"`
	TotalElements int64 `json:"total_elements"`
}

// ErrorInfo describes an error.
type ErrorInfo struct {
	StatusCode int    `json:"status_code"`
	Code       string `json:"code"`
	Message    string `json:"message"`
}

// ErrorResponse is the error envelope.
type ErrorResponse struct {
	Success bool      `json:"success"`
	Error   ErrorInfo `json:"error"`
}

// Message is a simple message payload.
type Message struct {
	Message string `json:"message"`
}

// ErrorHandler renders every error as the envelope. Internal error details are logged by Okapi and
// not echoed for 5xx responses.
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
