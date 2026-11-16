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

// Rejected reports whether the provider refused the request itself, so resending it unchanged cannot succeed.
func Rejected(err error) bool {
	switch statusOf(err) {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity:
		return true
	}
	return false
}

// ContextTooLong reports whether the request exceeds the model's context window or the provider's request size.
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
