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
