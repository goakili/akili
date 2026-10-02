// SPDX-FileCopyrightText: 2026 Jonas Kaninda
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package llm implements the model providers behind the control plane's LLM gateway. Agents never
// talk to a provider: they send provider-neutral requests over their tunnel, and the gateway picks
// the provider, applies budgets and records usage.
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

// Result is a completed model call.
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
