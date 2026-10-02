// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: Apache-2.0

// Package proto is the wire contract between the Akili control plane and its agents.
//
// Both sides import it and nothing else in common, so every message that crosses the tunnel is
// defined here. Messages travel as newline-delimited JSON envelopes over yamux streams.
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

// Version is the protocol version carried in every envelope. Bump it on a breaking change; peers
// reject envelopes from a newer major version rather than misreading them.
const Version = 1

// MaxEnvelopeSize bounds one encoded envelope. Tool output is truncated well below this, so a larger
// line means a broken or hostile peer.
const MaxEnvelopeSize = 8 << 20

// Envelope is the unit of every stream.
type Envelope struct {
	V       int             `json:"v"`
	Type    string          `json:"type"`
	ID      string          `json:"id,omitempty"`
	TS      time.Time       `json:"ts"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// ErrVersion is returned when a peer speaks a newer protocol version.
var ErrVersion = errors.New("proto: unsupported protocol version")

// NewEnvelope marshals payload into an envelope of the given type.
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

// Decode unmarshals the envelope payload into v.
func (e Envelope) Decode(v any) error {
	if len(e.Payload) == 0 {
		return nil
	}
	if err := json.Unmarshal(e.Payload, v); err != nil {
		return fmt.Errorf("proto: decode %s: %w", e.Type, err)
	}
	return nil
}

// Conn frames envelopes over a byte stream. Send is safe for concurrent use; Recv is not (one reader
// per stream).
type Conn struct {
	rwc io.ReadWriteCloser
	r   *bufio.Reader
	wmu sync.Mutex
}

// NewConn wraps a stream (typically a yamux stream).
func NewConn(rwc io.ReadWriteCloser) *Conn {
	return &Conn{rwc: rwc, r: bufio.NewReaderSize(rwc, 64<<10)}
}

// Send writes one envelope.
func (c *Conn) Send(typ string, payload any) error {
	env, err := NewEnvelope(typ, payload)
	if err != nil {
		return err
	}
	return c.SendEnvelope(env)
}

// SendEnvelope writes a pre-built envelope.
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

// Recv reads the next envelope.
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

// readLine reads up to '\n', refusing lines larger than MaxEnvelopeSize.
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

// Close closes the underlying stream.
func (c *Conn) Close() error { return c.rwc.Close() }
