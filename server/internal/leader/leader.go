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
