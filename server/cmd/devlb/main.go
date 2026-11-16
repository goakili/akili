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

// Command devlb is a round-robin TCP load balancer for local multi-replica tests.
package main

import (
	"flag"
	"io"
	"log"
	"net"
	"strings"
	"sync/atomic"
	"time"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:18700", "listen address")
	backends := flag.String("backends", "127.0.0.1:18701,127.0.0.1:18702", "comma-separated backend addresses")
	flag.Parse()
	targets := strings.Split(*backends, ",")
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("devlb on %s → %v", *addr, targets)
	var next atomic.Uint64
	for {
		c, err := ln.Accept()
		if err != nil {
			log.Fatal(err)
		}
		go func() {
			defer c.Close()
			start := next.Add(1)
			for i := range targets {
				t := targets[(int(start)+i)%len(targets)]
				b, err := net.DialTimeout("tcp", t, 2*time.Second)
				if err != nil {
					continue
				}
				defer b.Close()
				pipe(c, b)
				return
			}
			log.Printf("no backend available for %s", c.RemoteAddr())
		}()
	}
}

// pipe copies both ways and closes both ends when either side ends, so a dead backend drops the client.
func pipe(a, b net.Conn) {
	done := make(chan struct{}, 2)
	cp := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		done <- struct{}{}
	}
	go cp(a, b)
	go cp(b, a)
	<-done
	_ = a.Close()
	_ = b.Close()
}
