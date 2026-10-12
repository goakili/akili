// Copyright (c) 2026 Jonas Kaninda.
//
// This file is part of Akili.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.
//
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package leader elects one control-plane replica to run singleton loops (dispatcher, sweeper,
// scheduler) using a Redis lease.
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

// Elector holds or waits for the lease.
type Elector struct {
	rdb     *redis.Client
	key     string
	token   string
	ttl     time.Duration
	leading atomic.Bool
}

// New returns an elector for the named role.
func New(rdb *redis.Client, name string) *Elector {
	return &Elector{rdb: rdb, key: "akili:leader:" + name, token: rand.Text(), ttl: 15 * time.Second}
}

// Leading reports whether this replica currently holds the lease.
func (e *Elector) Leading() bool { return e.leading.Load() }

// Run campaigns until ctx ends. The lease is renewed at a third of its TTL, so a leader that stalls
// longer than the TTL loses it before a second leader can act for long.
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
