//go:build windows

package ipc

import (
	"net"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// peerOf must report the process on the other end, not this one's idea of
// itself — the whole peer check is downstream of it being right. Windows has
// no uid to report; the pipe's DACL pins the account.
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
}

// fdConn is a connection whose descriptor is no named pipe.
type fdConn struct{ net.Conn }

func (fdConn) Fd() uintptr { return 0 }

// A connection with no handle, or one whose handle is no pipe, has no peer to
// read.
func TestPeerOfFailsWithoutAPipe(t *testing.T) {
	a, b := net.Pipe()
	t.Cleanup(func() { a.Close(); b.Close() })

	_, err := peerOf(a)
	assert.Error(t, err)
	_, err = peerOf(fdConn{a})
	assert.Error(t, err)
}
