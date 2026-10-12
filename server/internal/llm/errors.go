// Copyright (c) 2026 Jonas Kaninda.
//
// This file is part of Akili.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package llm

import (
	"errors"
	"net/http"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// StatusError is an HTTP error answer from a provider.
type StatusError struct {
	Code int
	Msg  string
}

func (e *StatusError) Error() string { return e.Msg }

func statusOf(err error) int {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode
	}
	var se *StatusError
	if errors.As(err, &se) {
		return se.Code
	}
	return 0
}

// Rejected reports whether the provider refused the request itself (bad request, auth, unknown
// model), so resending it unchanged cannot succeed. Overload, rate limits and network errors are not.
func Rejected(err error) bool {
	switch statusOf(err) {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity:
		return true
	}
	return false
}

// ContextTooLong reports whether the request was refused because the conversation exceeds the
// model's context window or the provider's request size.
func ContextTooLong(err error) bool {
	switch statusOf(err) {
	case http.StatusRequestEntityTooLarge:
		return true
	case http.StatusBadRequest:
		msg := strings.ToLower(err.Error())
		for _, s := range []string{"prompt is too long", "context length", "context window", "maximum context", "too many tokens", "input is too long"} {
			if strings.Contains(msg, s) {
				return true
			}
		}
	}
	return false
}
