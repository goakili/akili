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
