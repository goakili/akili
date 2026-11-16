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

// Package proto is the wire contract between the Akili control plane and its agents.
package proto

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

// Version is bumped on a breaking change; peers reject newer versions rather than misread them.
const Version = 1

// MaxEnvelopeSize is far above truncated tool output, so a larger line means a broken or hostile peer.
const MaxEnvelopeSize = 8 << 20

type Envelope struct {
	V       int             `json:"v"`
	Type    string          `json:"type"`
	ID      string          `json:"id,omitempty"`
	TS      time.Time       `json:"ts"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

var ErrVersion = errors.New("proto: unsupported protocol version")

func NewEnvelope(typ string, payload any) (Envelope, error) {
	env := Envelope{V: Version, Type: typ, TS: time.Now().UTC()}
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return env, fmt.Errorf("proto: marshal %s: %w", typ, err)
		}
		env.Payload = b
	}
	return env, nil
}

func (e Envelope) Decode(v any) error {
	if len(e.Payload) == 0 {
		return nil
	}
	if err := json.Unmarshal(e.Payload, v); err != nil {
		return fmt.Errorf("proto: decode %s: %w", e.Type, err)
	}
	return nil
}

// Conn's Send is safe for concurrent use; Recv is not (one reader per stream).
type Conn struct {
	rwc io.ReadWriteCloser
	r   *bufio.Reader
	wmu sync.Mutex
}

func NewConn(rwc io.ReadWriteCloser) *Conn {
	return &Conn{rwc: rwc, r: bufio.NewReaderSize(rwc, 64<<10)}
}

func (c *Conn) Send(typ string, payload any) error {
	env, err := NewEnvelope(typ, payload)
	if err != nil {
		return err
	}
	return c.SendEnvelope(env)
}

func (c *Conn) SendEnvelope(env Envelope) error {
	if env.V == 0 {
		env.V = Version
	}
	b, err := json.Marshal(env)
	if err != nil {
		return err
	}
	if len(b) > MaxEnvelopeSize {
		return fmt.Errorf("proto: envelope %s too large (%d bytes)", env.Type, len(b))
	}
	b = append(b, '\n')
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err = c.rwc.Write(b)
	return err
}

func (c *Conn) Recv() (Envelope, error) {
	var env Envelope
	line, err := c.readLine()
	if err != nil {
		return env, err
	}
	if err := json.Unmarshal(line, &env); err != nil {
		return env, fmt.Errorf("proto: bad envelope: %w", err)
	}
	if env.V > Version {
		return env, ErrVersion
	}
	return env, nil
}

func (c *Conn) readLine() ([]byte, error) {
	var buf []byte
	for {
		chunk, err := c.r.ReadSlice('\n')
		buf = append(buf, chunk...)
		if len(buf) > MaxEnvelopeSize {
			return nil, fmt.Errorf("proto: envelope exceeds %d bytes", MaxEnvelopeSize)
		}
		if err == nil {
			return buf[:len(buf)-1], nil
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			if len(buf) > 0 && errors.Is(err, io.EOF) {
				return buf, nil
			}
			return nil, err
		}
	}
}

func (c *Conn) Close() error { return c.rwc.Close() }
