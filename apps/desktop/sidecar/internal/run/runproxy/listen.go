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

package runproxy

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
)

// tlsHandshake is the first byte of a TLS handshake record, which no HTTP
// request starts with.
const tlsHandshake = 0x16

var errNoCertificate = errors.New("runproxy: a TLS handshake on a server with no certificate")

// IsTLS is whether r came over TLS. http.Server never sees the handshake, so
// r.TLS is nil either way.
func IsTLS(r *http.Request) bool {
	c, _ := r.Context().Value(connKey{}).(*sniffConn)
	return c != nil && c.tls.Load() != nil
}

type connKey struct{}

// sniffListener hands each connection over as a sniffConn.
type sniffListener struct {
	net.Listener
	config *tls.Config // nil for a server with no certificate
}

func (l *sniffListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &sniffConn{Conn: c, config: l.config}, nil
}

// sniffConn is a connection whose first byte decides its transport. The byte
// is read on the first Read, under whatever deadline the server set, so the
// accept loop never waits on it; a handshake runs there too.
type sniffConn struct {
	net.Conn
	config *tls.Config
	once   sync.Once
	plain  net.Conn // the connection with its first byte put back
	err    error
	tls    atomic.Pointer[tls.Conn] // set once a handshake completes
}

func (c *sniffConn) Read(p []byte) (int, error) {
	c.once.Do(c.sniff)
	if c.err != nil {
		return 0, c.err
	}
	if tc := c.tls.Load(); tc != nil {
		return tc.Read(p)
	}
	return c.plain.Read(p)
}

// Write goes over TLS once a handshake completes. The server writes only
// after it has read a request, so it never writes before the sniff.
func (c *sniffConn) Write(p []byte) (int, error) {
	if tc := c.tls.Load(); tc != nil {
		return tc.Write(p)
	}
	return c.Conn.Write(p)
}

// Close sends a TLS connection's close_notify, so its client reads a clean
// end.
func (c *sniffConn) Close() error {
	if tc := c.tls.Load(); tc != nil {
		return tc.Close()
	}
	return c.Conn.Close()
}

// CloseWrite half-closes, as http.Server does to let a client read an answer
// sent before its whole request was read.
func (c *sniffConn) CloseWrite() error {
	if tc := c.tls.Load(); tc != nil {
		return tc.CloseWrite()
	}
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return nil
}

func (c *sniffConn) sniff() {
	var first [1]byte
	if _, err := io.ReadFull(c.Conn, first[:]); err != nil {
		c.err = err
		return
	}
	c.plain = &prefixConn{Conn: c.Conn, first: first[:]}
	if first[0] != tlsHandshake {
		return
	}
	if c.config == nil {
		c.err = errNoCertificate
		_ = c.Conn.Close()
		return
	}
	tc := tls.Server(c.plain, c.config)
	if err := tc.Handshake(); err != nil {
		// Closed, so the server's plain-text 400 never reaches a TLS client.
		c.err = err
		_ = c.Conn.Close()
		return
	}
	c.tls.Store(tc)
}

// prefixConn is a connection whose first bytes were already read.
type prefixConn struct {
	net.Conn
	first []byte
}

func (c *prefixConn) Read(p []byte) (int, error) {
	if len(c.first) > 0 {
		n := copy(p, c.first)
		c.first = c.first[n:]
		return n, nil
	}
	return c.Conn.Read(p)
}

// connContext puts the connection in each of its requests' context, for
// IsTLS.
func connContext(ctx context.Context, c net.Conn) context.Context {
	return context.WithValue(ctx, connKey{}, c)
}
