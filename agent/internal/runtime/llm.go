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

package runtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"

	"github.com/goakili/akili/proto"
)

// LLM carries no credentials: the gateway identifies the agent by the tunnel the request arrives on.
type LLM struct {
	client *http.Client
}

// NewLLM returns a client that opens a new tunnel stream per request.
func NewLLM(open func() (net.Conn, error)) *LLM {
	return &LLM{client: &http.Client{Transport: &http.Transport{
		DialContext:       func(context.Context, string, string) (net.Conn, error) { return open() },
		DisableKeepAlives: true,
	}}}
}

// FinalError is a gateway error the agent must not retry as is (budget, kill switch, closed session, refusal).
type FinalError struct {
	Code, Message string
}

func (e *FinalError) Error() string { return e.Message }

func (l *LLM) Complete(ctx context.Context, req proto.LLMRequest, onDelta func(kind, text string)) (*proto.LLMEvent, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://control-plane"+proto.InternalLLMPath, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	hr.Header.Set("Content-Type", "application/json")
	resp, err := l.client.Do(hr)
	if err != nil {
		return nil, fmt.Errorf("gateway: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("gateway: %s: %s", resp.Status, bytes.TrimSpace(b))
	}
	r := bufio.NewReaderSize(resp.Body, 64<<10)
	for {
		line, err := r.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var ev proto.LLMEvent
			if jerr := json.Unmarshal(line, &ev); jerr != nil {
				return nil, fmt.Errorf("gateway: bad event: %w", jerr)
			}
			switch ev.Type {
			case proto.LLMEventDelta:
				if onDelta != nil {
					onDelta(ev.Kind, ev.Text)
				}
			case proto.LLMEventMessage:
				if ev.Message == nil {
					return nil, errors.New("gateway: message event without a message")
				}
				return &ev, nil
			case proto.LLMEventError:
				switch ev.Code {
				case proto.CodeBudgetExhausted, proto.CodeSessionClosed, proto.CodeNoProvider, proto.CodeProviderRejected, proto.CodeContextTooLong:
					return nil, &FinalError{Code: ev.Code, Message: ev.Error}
				}
				return nil, errors.New(ev.Error)
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, errors.New("gateway: stream ended without a result")
			}
			return nil, err
		}
	}
}
