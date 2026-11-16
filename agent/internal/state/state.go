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
