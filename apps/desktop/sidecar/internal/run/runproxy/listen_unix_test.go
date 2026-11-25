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

//go:build !windows

package runproxy

import (
	"crypto/tls"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// unixPair is a connected pair of Unix sockets, closed when the test ends.
func unixPair(t *testing.T) (server, client net.Conn) {
	t.Helper()
	// /tmp, since macOS's per-user temp directory is too long for a socket.
	dir, err := os.MkdirTemp("/tmp", "runproxy")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	ln, err := net.Listen("unix", filepath.Join(dir, "s"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	accepted := make(chan net.Conn, 1)
	go func() {
		c, _ := ln.Accept()
		accepted <- c
	}()
	client, err = net.Dial("unix", ln.Addr().String())
	require.NoError(t, err)
	server = testutil.Recv(t, accepted, "the accept")
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	return server, client
}

// A plain connection half-closes as its socket does, so a server answering
// before it read a whole request lets the client read the answer.
func TestAPlainConnectionHalfCloses(t *testing.T) {
	server, client := unixPair(t)
	c := &sniffConn{Conn: server}
	_, err := client.Write([]byte("GET"))
	require.NoError(t, err)
	_, err = c.Read(make([]byte, 1))
	require.NoError(t, err)

	cw, ok := any(c).(interface{ CloseWrite() error })
	require.True(t, ok, "sniffConn half-closes")
	require.NoError(t, cw.CloseWrite())
	b, err := io.ReadAll(client)
	require.NoError(t, err)
	assert.Empty(t, b, "the client reads the server's end")
	_, err = client.Write([]byte(" /"))
	assert.NoError(t, err, "and can still write")
}

// tlsPair is a sniffConn that completed a handshake with its client, which
// it answers.
func tlsPair(t *testing.T) (*sniffConn, *tls.Conn) {
	t.Helper()
	cert, err := NewCertificate(time.Now())
	require.NoError(t, err)
	server, client := unixPair(t)
	c := &sniffConn{Conn: server, config: &tls.Config{Certificates: []tls.Certificate{cert.TLS}}}
	config := trusting(cert)
	config.ServerName = "127.0.0.1"
	tc := tls.Client(client, config)
	served := make(chan error, 1)
	go func() {
		_, err := c.Read(make([]byte, 1))
		served <- err
	}()
	require.NoError(t, tc.Handshake())
	_, err = tc.Write([]byte("x"))
	require.NoError(t, err)
	require.NoError(t, testutil.Recv(t, served, "the server's read"))
	return c, tc
}

// Closing a TLS connection sends close_notify, so the client reads a clean end.
func TestATLSConnectionClosesWithCloseNotify(t *testing.T) {
	c, tc := tlsPair(t)
	require.NoError(t, c.Close())
	_, err := tc.Read(make([]byte, 1))
	assert.ErrorIs(t, err, io.EOF)
}

// A TLS connection half-closes through TLS, so the client reads a clean end
// and can still write.
func TestATLSConnectionHalfCloses(t *testing.T) {
	c, tc := tlsPair(t)
	require.NoError(t, c.CloseWrite())
	_, err := tc.Read(make([]byte, 1))
	assert.ErrorIs(t, err, io.EOF)
	_, err = tc.Write([]byte("y"))
	assert.NoError(t, err)
}
