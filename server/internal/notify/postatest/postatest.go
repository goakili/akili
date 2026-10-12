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

// Package postatest is a fake Posta API for tests: it accepts sends with one API key and records them.
package postatest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// Sent is one email the fake accepted.
type Sent struct {
	ID      string   `json:"id"`
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Text    string   `json:"text"`
	HTML    string   `json:"html"`
	DryRun  bool     `json:"dry_run"`
}

// Server is the fake. GET /_fake/sent lists what it accepted.
type Server struct {
	key  string
	mu   sync.Mutex
	sent []Sent
}

// New returns a fake that accepts the API key key.
func New(key string) *Server { return &Server{key: key, sent: []Sent{}} }

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/_fake/sent":
		s.mu.Lock()
		defer s.mu.Unlock()
		_ = json.NewEncoder(w).Encode(s.sent)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/emails/send":
		if r.Header.Get("Authorization") != "Bearer "+s.key {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"success":false,"error":{"code":"unauthorized","message":"invalid API key"}}`))
			return
		}
		var m Sent
		if err := json.NewDecoder(r.Body).Decode(&m); err != nil || m.From == "" || len(m.To) == 0 || m.Subject == "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"success":false,"error":{"message":"from, to and subject are required"}}`))
			return
		}
		m.DryRun = r.URL.Query().Get("dry_run") == "true"
		s.mu.Lock()
		m.ID = fmt.Sprintf("00000000-0000-4000-8000-%012d", len(s.sent)+1)
		s.sent = append(s.sent, m)
		s.mu.Unlock()
		if m.DryRun {
			_, _ = w.Write([]byte(`{"success":true,"data":{"valid":true}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]string{"id": m.ID, "status": "queued"}})
	default:
		http.NotFound(w, r)
	}
}
