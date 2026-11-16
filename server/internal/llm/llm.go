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

// Package llm implements the model providers behind the control plane's LLM gateway.
package llm

import (
	"context"
	"strings"

	"github.com/goakili/akili/proto"
)

// Request is a provider-neutral model call.
type Request struct {
	Model     string
	System    string
	Messages  []proto.Message
	Tools     []proto.ToolDef
	MaxTokens int
	Effort    string
}

// Usage is token accounting, including prompt-cache traffic priced differently from plain input.
type Usage struct {
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
}

type Result struct {
	Message    proto.Message
	StopReason string
	Usage      Usage
}

// DeltaFunc receives streamed output: kind is proto.DeltaText or proto.DeltaThinking.
type DeltaFunc func(kind, text string)

// Provider streams one model call, calling onDelta for each piece of output.
type Provider interface {
	Stream(ctx context.Context, req Request, onDelta DeltaFunc) (Result, error)
}

// DefaultContextTokens is the context window assumed when a provider does not set one.
const DefaultContextTokens = 200_000

// minHistoryTokens keeps a usable history even when a provider's settings leave little room.
const minHistoryTokens = 16_000

// HistoryTokens is the context left for history after the output and the fixed prefix (about 3 characters per token).
func HistoryTokens(contextTokens, maxOutput, prefixChars int) int {
	if contextTokens <= 0 {
		contextTokens = DefaultContextTokens
	}
	return max(contextTokens-maxOutput-prefixChars/3-2_000, minHistoryTokens)
}

// Price is USD per million tokens.
type Price struct {
	Input, Output float64
}

// knownPrices are list prices for first-party models; a provider row can override them.
var knownPrices = map[string]Price{
	"claude-fable-5-1":  {10, 50},
	"claude-opus-5-5":   {4, 20},
	"claude-opus-5":     {5, 25},
	"claude-sonnet-5-5": {2, 10},
	"claude-sonnet-5":   {2, 10},
	"claude-haiku-4-5":  {1, 5},
}

// PriceFor returns the price for a model, preferring an explicit override.
func PriceFor(model string, override Price) Price {
	if override.Input > 0 || override.Output > 0 {
		return override
	}
	if p, ok := knownPrices[model]; ok {
		return p
	}
	for m, p := range knownPrices {
		if strings.HasPrefix(model, m) {
			return p
		}
	}
	return Price{}
}

// Cost prices a call. Cache writes cost 1.25x input and cache reads 0.1x input.
func Cost(u Usage, p Price) float64 {
	in := float64(u.InputTokens) + 1.25*float64(u.CacheWriteTokens) + 0.1*float64(u.CacheReadTokens)
	return (in*p.Input + float64(u.OutputTokens)*p.Output) / 1_000_000
}
