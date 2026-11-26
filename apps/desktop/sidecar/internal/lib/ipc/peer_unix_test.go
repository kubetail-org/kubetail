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

package ipc

import (
	"net"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// peerOf must report the process on the other end, not this one's idea of
// itself — the whole peer check is downstream of it being right.
func TestPeerOf_ReportsConnectingProcess(t *testing.T) {
	path := testEndpoint(t)
	ln, err := Listen(path)
	require.NoError(t, err)
	defer ln.Close()

	accepted := acceptOne(ln)
	client, err := dialEndpoint(path)
	require.NoError(t, err)
	defer client.Close()

	server, ok := <-accepted
	require.True(t, ok)
	defer server.Close()

	p, err := peerOf(server)
	require.NoError(t, err)
	assert.Equal(t, os.Getpid(), p.pid)
	assert.Equal(t, os.Getuid(), p.uid)
}

// A descriptor that is not a socket has no peer to read.
func TestPeerCredsFailsOffASocket(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { r.Close(); w.Close() })

	_, err = peerCreds(r.Fd())
	assert.Error(t, err)
}

// A connection that is no socket, or whose socket is gone, has no credentials to read.
func TestPeerOfFailsWithoutASocket(t *testing.T) {
	a, b := net.Pipe()
	t.Cleanup(func() { a.Close(); b.Close() })
	_, err := peerOf(a)
	assert.ErrorContains(t, err, "peer credentials unavailable")

	_, err = peerOf(&net.UnixConn{})
	assert.Error(t, err, "a connection with no descriptor")

	path := testEndpoint(t)
	ln, err := Listen(path)
	require.NoError(t, err)
	defer ln.Close()
	accepted := acceptOne(ln)
	client, err := dialEndpoint(path)
	require.NoError(t, err)
	defer client.Close()
	server, ok := <-accepted
	require.True(t, ok)
	require.NoError(t, server.Close())
	_, err = peerOf(server)
	assert.ErrorContains(t, err, "control")
}
