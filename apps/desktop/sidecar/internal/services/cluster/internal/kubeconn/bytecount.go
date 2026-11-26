// Copyright 2024 The Kubetail Authors
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

// The byte counts a connection keeps of the wire traffic on its sockets.
package kubeconn

import (
	"context"
	"net"
	"sync/atomic"
	"time"
)

// byteCounts is what one connection has received and sent on the wire.
type byteCounts struct {
	received atomic.Uint64
	sent     atomic.Uint64
}

// newCountingDialer dials as client-go does by default and counts every byte on the sockets
// it opens. Set as the rest.Config's Dial, it sits under every transport client-go builds from
// the config: HTTP/1.1, HTTP/2, and the SPDY and WebSocket upgrades exec and port-forward use.
// TLS is layered above it, so the counts are ciphertext.
func newCountingDialer(counts *byteCounts) func(context.Context, string, string) (net.Conn, error) {
	d := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		c, err := d.DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		return &countingConn{Conn: c, counts: counts}, nil
	}
}

// countingConn adds what each Read and Write moves to counts.
type countingConn struct {
	net.Conn
	counts *byteCounts
}

func (c *countingConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.counts.received.Add(uint64(n))
	}
	return n, err
}

func (c *countingConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if n > 0 {
		c.counts.sent.Add(uint64(n))
	}
	return n, err
}
