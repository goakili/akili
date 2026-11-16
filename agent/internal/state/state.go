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

// Package state persists the agent's identity in a 0600 file that never contains the join token.
package state

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const FileName = "state.json"

type State struct {
	URL          string `json:"url"`
	AgentID      string `json:"agent_id"`
	Name         string `json:"name"`
	PrivateKey   []byte `json:"private_key"`
	CPSigningKey []byte `json:"cp_signing_key"`
	Workdir      string `json:"workdir"`
	CACert       string `json:"ca_cert,omitempty"`
	Insecure     bool   `json:"insecure,omitempty"`
	// Dir is where the state was loaded from (not stored).
	Dir string `json:"-"`
}

func (s *State) Key() (ed25519.PrivateKey, error) {
	if len(s.PrivateKey) != ed25519.PrivateKeySize {
		return nil, errors.New("state: invalid private key")
	}
	return ed25519.PrivateKey(s.PrivateKey), nil
}

func Load(dir string) (*State, error) {
	b, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("agent is not enrolled (no %s in %s); run `akili-agent enroll` first", FileName, dir)
		}
		return nil, err
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("state: %w", err)
	}
	if s.AgentID == "" || s.URL == "" {
		return nil, errors.New("state: incomplete; re-enroll the agent")
	}
	s.Dir = dir
	return &s, nil
}

// Save writes the state file atomically with 0600 permissions.
func Save(dir string, s *State) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, FileName+".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, FileName))
}
