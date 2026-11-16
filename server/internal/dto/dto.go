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
