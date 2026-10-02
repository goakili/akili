// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package state persists the agent's identity: its Ed25519 key, id, control-plane URL and the pinned
// policy-signing key. The file is written 0600 and never contains the join token.
package state

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// FileName is the state file inside the state directory.
const FileName = "state.json"

// State is the persisted identity.
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

// Key returns the agent's private key.
func (s *State) Key() (ed25519.PrivateKey, error) {
	if len(s.PrivateKey) != ed25519.PrivateKeySize {
		return nil, errors.New("state: invalid private key")
	}
	return ed25519.PrivateKey(s.PrivateKey), nil
}

// Load reads the state file.
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
