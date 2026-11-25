package ipc

import (
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// listenAuth binds a real endpoint (UDS or named pipe) behind the policy, and
// returns the wrapped listener with the address a client dials.
func listenAuth(t *testing.T, p Policy) (net.Listener, string) {
	t.Helper()
	path := testEndpoint(t)
	base, err := Listen(path)
	require.NoError(t, err)
	ln := Authenticated(base, p)
	t.Cleanup(func() { _ = ln.Close() })
	return ln, path
}

// The point of the whole package: a process that is not the host gets its
// connection closed before it can speak a byte of GraphQL or gRPC.
func TestAuthenticated_RejectsForeignPeer(t *testing.T) {
	ln, path := listenAuth(t, Policy{HostPID: os.Getpid() + 1})

	// A served peer would get the byte back; a rejected one gets a closed
	// connection. Either way the Read below returns, so nothing waits on a clock.
	accepted := acceptOne(ln)
	client, err := dialEndpoint(path)
	require.NoError(t, err)
	defer client.Close()
	go func() {
		if conn, ok := <-accepted; ok {
			_, _ = conn.Write([]byte{42})
		}
	}()

	_, err = client.Read(make([]byte, 1))
	assert.Error(t, err, "rejected peer must not be served")
}

// A rejection is not a listener error: returning it from Accept would let any
// unauthorized process kill the sidecar's serving loop.
func TestAuthenticated_KeepsAcceptingAfterRejection(t *testing.T) {
	ln, path := listenAuth(t, Policy{HostPID: os.Getpid()})

	// Reject the first peer only, so the second exercises the recovery path.
	first := true
	ln.(*authListener).peerOf = func(net.Conn) (peer, error) {
		if first {
			first = false
			return peer{pid: os.Getpid() + 1, uid: os.Getuid()}, nil
		}
		return peer{pid: os.Getpid(), uid: os.Getuid()}, nil
	}

	// One Accept call spans both connections: it rejects the first and returns
	// the second.
	accepted := acceptOne(ln)

	rejected, err := dialEndpoint(path)
	require.NoError(t, err)
	defer rejected.Close()

	assertRoundTrip(t, accepted, path)

	_, err = rejected.Read(make([]byte, 1))
	assert.Error(t, err, "first peer should have been rejected")
}

func TestAuthenticated_AcceptsHostPeer(t *testing.T) {
	ln, path := listenAuth(t, Policy{HostPID: os.Getpid()})
	assertRoundTrip(t, acceptOne(ln), path)
}

// A standalone sidecar run has no host pid to pin, and falls back to the uid.
func TestAuthenticated_WithoutHostPIDAcceptsSameUID(t *testing.T) {
	ln, path := listenAuth(t, Policy{})
	assertRoundTrip(t, acceptOne(ln), path)
}

// A closed listener still reports its error, so Serve can exit.
func TestAuthenticated_PropagatesListenerClose(t *testing.T) {
	ln, _ := listenAuth(t, Policy{})
	require.NoError(t, ln.Close())

	_, err := ln.Accept()
	require.Error(t, err)
	assert.True(t, errors.Is(err, net.ErrClosed), "got %v", err)
}

// assertRoundTrip dials the endpoint and checks a byte survives the trip, i.e.
// the connection was accepted and handed to the server.
func assertRoundTrip(t *testing.T, accepted <-chan net.Conn, path string) {
	t.Helper()
	client, err := dialEndpoint(path)
	require.NoError(t, err)
	defer client.Close()
	// Failsafe: a read that never lands means the wrong connection was served,
	// which would otherwise hang instead of failing.
	require.NoError(t, client.SetReadDeadline(time.Now().Add(testutil.Timeout)))

	server, ok := <-accepted
	require.True(t, ok, "connection should have been accepted")
	defer server.Close()

	// Written from its own goroutine: a Windows named pipe holds the write until someone
	// reads, and the reader is this goroutine.
	written := make(chan error, 1)
	go func() {
		_, err := server.Write([]byte{42})
		written <- err
	}()

	buf := make([]byte, 1)
	_, err = client.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, byte(42), buf[0])
	require.NoError(t, <-written)
}

// A peer is refused on anything it cannot prove: credentials the kernel would not
// give, or another account, whatever its pid.
func TestRejectNamesWhyAPeerIsRefused(t *testing.T) {
	refuse := func(p peer, err error) string {
		l := &authListener{peerOf: func(net.Conn) (peer, error) { return p, err }}
		return l.reject(nil)
	}

	assert.Equal(t, "peer credentials unavailable: no creds", refuse(peer{}, errors.New("no creds")))
	assert.Equal(t, "uid mismatch", refuse(peer{pid: os.Getpid(), uid: os.Getuid() + 1}, nil))
	assert.Empty(t, refuse(peer{pid: os.Getpid(), uid: os.Getuid()}, nil))
}
