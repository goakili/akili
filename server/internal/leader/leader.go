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

// Package leader elects one control-plane replica to run singleton loops using a Redis lease.
package leader

import (
	"context"
	"crypto/rand"
	"sync/atomic"
	"time"

	"github.com/jkaninda/logger"
	"github.com/redis/go-redis/v9"
)

var acquireScript = redis.NewScript(`
local v = redis.call("get", KEYS[1])
if not v then
  redis.call("set", KEYS[1], ARGV[1], "PX", ARGV[2])
  return 1
end
if v == ARGV[1] then
  return redis.call("pexpire", KEYS[1], ARGV[2])
end
return 0`)

var releaseScript = redis.NewScript(`
if redis.call("get", KEYS[1]) == ARGV[1] then
  return redis.call("del", KEYS[1])
end
return 0`)

type Elector struct {
	rdb     *redis.Client
	key     string
	token   string
	ttl     time.Duration
	leading atomic.Bool
}

func New(rdb *redis.Client, name string) *Elector {
	return &Elector{rdb: rdb, key: "akili:leader:" + name, token: rand.Text(), ttl: 15 * time.Second}
}

func (e *Elector) Leading() bool { return e.leading.Load() }

// Run renews the lease at a third of its TTL, so a stalled leader loses it before a second leader acts for long.
func (e *Elector) Run(ctx context.Context) {
	t := time.NewTicker(e.ttl / 3)
	defer t.Stop()
	defer func() {
		_ = releaseScript.Run(context.Background(), e.rdb, []string{e.key}, e.token).Err()
		e.leading.Store(false)
	}()
	for {
		n, err := acquireScript.Run(ctx, e.rdb, []string{e.key}, e.token, e.ttl.Milliseconds()).Int()
		now := err == nil && n == 1
		if was := e.leading.Swap(now); was != now {
			logger.Info("leadership changed", "role", e.key, "leading", now)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
