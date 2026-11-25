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

package kubeconn

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// countingListener counts the server's side of every socket it accepts, so a test can hold the
// client's counts to what actually crossed the wire: what the server received is what the
// client sent, and the other way round.
type countingListener struct {
	net.Listener
	counts *byteCounts
}

func (l *countingListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &countingConn{Conn: c, counts: l.counts}, nil
}

// countingServer serves h over sockets whose server side is counted.
func countingServer(t *testing.T, h http.HandlerFunc) (*httptest.Server, *byteCounts) {
	t.Helper()
	counts := &byteCounts{}
	srv := httptest.NewUnstartedServer(h)
	srv.Listener = &countingListener{Listener: srv.Listener, counts: counts}
	srv.Start()
	t.Cleanup(srv.Close)
	return srv, counts
}

// connectionTo builds a real connection to srv, retired with the test.
func connectionTo(t *testing.T, srv *httptest.Server) *Connection {
	t.Helper()
	conn, err := NewConnection(&rest.Config{Host: srv.URL})
	require.NoError(t, err)
	t.Cleanup(conn.Retire)
	return conn
}

// requireWireMatches holds conn's counts to server's. Each side adds to its count only after its
// socket call returns, so the peer can already hold the bytes; wait for both counts to land.
func requireWireMatches(t *testing.T, conn *Connection, server *byteCounts) {
	t.Helper()
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		received, sent := conn.Bytes()
		assert.Equal(c, server.sent.Load(), received)
		assert.Equal(c, server.received.Load(), sent)
	}, 5*time.Second, time.Millisecond)
}

// Every byte on the wire counts, headers included, which is why the expectation is the server's
// view of the same sockets rather than a body length.
func TestBytesCountTheWireBothWays(t *testing.T) {
	t.Parallel()
	srv, server := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = io.WriteString(w, `{}`)
	})
	conn := connectionTo(t, srv)

	body := []byte(`{"kind":"SelfSubjectReview"}`)
	require.NoError(t, conn.postJSON(context.Background(), "/apis/authentication.k8s.io/v1/selfsubjectreviews", body, &struct{}{}))

	requireWireMatches(t, conn, server)
	_, sent := conn.Bytes()
	assert.Greater(t, sent, uint64(len(body)))
}

// The case the counter exists for: a watch sends for as long as it is open, so its bytes have
// to count as they arrive rather than when the stream ends.
func TestBytesCountAStreamWhileItIsOpen(t *testing.T) {
	t.Parallel()
	const chunk = "first event\n"
	release := make(chan struct{})
	srv, _ := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, chunk)
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	t.Cleanup(func() { close(release) })
	conn := connectionTo(t, srv)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/api/v1/pods?watch=true", nil)
	require.NoError(t, err)
	resp, err := conn.HTTPClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	_, err = io.ReadFull(resp.Body, make([]byte, len(chunk)))
	require.NoError(t, err)

	received, _ := conn.Bytes()
	assert.Greater(t, received, uint64(len(chunk)))
}

// exec and port-forward build their own transport from Config; the counter has to sit under it.
func TestBytesCountAClientBuiltFromConfig(t *testing.T) {
	t.Parallel()
	srv, server := countingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})
	conn := connectionTo(t, srv)

	own, err := rest.HTTPClientFor(conn.Config)
	require.NoError(t, err)
	t.Cleanup(own.CloseIdleConnections)
	resp, err := own.Get(srv.URL + "/version")
	require.NoError(t, err)
	_, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	requireWireMatches(t, conn, server)
}

// An upgraded stream is raw bytes on the socket once the handshake is done. The counter is the
// socket, so it needs no protocol at all to count them.
func TestBytesCountRawBytesOnTheSocket(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_, _ = io.Copy(c, c)
	}()

	var counts byteCounts
	c, err := newCountingDialer(&counts)(context.Background(), "tcp", ln.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	_, err = io.WriteString(c, "ping")
	require.NoError(t, err)
	_, err = io.ReadFull(c, make([]byte, 4))
	require.NoError(t, err)

	assert.Equal(t, uint64(4), counts.sent.Load())
	assert.Equal(t, uint64(4), counts.received.Load())
}

func TestBytesPassADialFailureThrough(t *testing.T) {
	t.Parallel()
	// A port that was listening a moment ago and is not now.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	require.NoError(t, ln.Close())

	var counts byteCounts
	c, err := newCountingDialer(&counts)(context.Background(), "tcp", ln.Addr().String())

	assert.Nil(t, c)
	assert.Error(t, err)
	assert.Zero(t, counts.sent.Load())
}

func TestBytesOnAConnectionNobodyBuiltAreZero(t *testing.T) {
	t.Parallel()
	received, sent := (&Connection{}).Bytes()
	assert.Zero(t, received)
	assert.Zero(t, sent)
}

// client-go's transport cache keeps each config's dialer in its key, so the dialer must not
// reach the connection, or a retired one is never collected.
func TestARetiredConnectionIsCollected(t *testing.T) {
	t.Parallel()
	conn, err := NewConnection(&rest.Config{Host: "https://127.0.0.1:1", TLSClientConfig: rest.TLSClientConfig{Insecure: true}})
	require.NoError(t, err)
	collected := make(chan struct{})
	runtime.AddCleanup(conn, func(ch chan struct{}) { close(ch) }, collected)
	conn.Retire()
	conn = nil

	require.Eventually(t, func() bool {
		runtime.GC()
		select {
		case <-collected:
			return true
		default:
			return false
		}
	}, testutil.Timeout, 10*time.Millisecond, "the retired connection to be collected")
}
