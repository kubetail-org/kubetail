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
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// transportHandler answers which transport its request came over.
var transportHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "tls=%v", IsTLS(r))
})

// serveOn serves srv on a loopback listener for the test and answers its
// address.
func serveOn(t *testing.T, srv *Server) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return ln.Addr().String()
}

// trusting is a client that trusts c alone and offers h2 first.
func trusting(c *Certificate) *tls.Config {
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(c.PEM)
	return &tls.Config{RootCAs: roots, NextProtos: []string{"h2", "http/1.1"}}
}

func body(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(b)
}

// A plain request and a TLS one share the listener, and each handler reads
// its transport. TLS offers http/1.1 alone.
func TestOneListenerServesBothTransports(t *testing.T) {
	c, err := NewCertificate(time.Now())
	require.NoError(t, err)
	addr := serveOn(t, NewServer(transportHandler, c))

	resp, err := http.Get("http://" + addr + "/")
	require.NoError(t, err)
	assert.Equal(t, "tls=false", body(t, resp))

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: trusting(c), ForceAttemptHTTP2: true}}
	resp, err = client.Get("https://" + addr + "/")
	require.NoError(t, err)
	require.NotNil(t, resp.TLS)
	assert.Equal(t, "http/1.1", resp.TLS.NegotiatedProtocol)
	assert.Equal(t, "tls=true", body(t, resp))
}

// A server with no certificate closes a connection that opens a handshake,
// and still serves plain HTTP.
func TestATLSConnectionWithNoCertificateIsClosed(t *testing.T) {
	addr := serveOn(t, NewServer(transportHandler, nil))
	c, err := NewCertificate(time.Now())
	require.NoError(t, err)
	_, err = tls.Dial("tcp", addr, trusting(c))
	assert.Error(t, err)

	resp, err := http.Get("http://" + addr + "/")
	require.NoError(t, err)
	assert.Equal(t, "tls=false", body(t, resp))
}

// The first byte is read under the header bound, never in the accept loop:
// a connection that sends nothing ends at the bound while another is served.
func TestASilentConnectionEndsAtTheHeaderTimeout(t *testing.T) {
	c, err := NewCertificate(time.Now())
	require.NoError(t, err)
	addr := serveOn(t, newServer(transportHandler, c, 50*time.Millisecond, time.Minute))

	silent, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = silent.Close() })
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		_, _ = io.Copy(io.Discard, silent)
	}()

	resp, err := http.Get("http://" + addr + "/")
	require.NoError(t, err)
	assert.Equal(t, "tls=false", body(t, resp))
	testutil.Wait(t, closed, "the silent connection to close")
}

// A handshake that fails closes its connection with nothing in plain text
// written to it, and the server serves on.
func TestAFailedHandshakeClosesTheConnection(t *testing.T) {
	c, err := NewCertificate(time.Now())
	require.NoError(t, err)
	addr := serveOn(t, NewServer(transportHandler, c))
	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	_, err = conn.Write([]byte{tlsHandshake, 'n', 'o', 't', ' ', 't', 'l', 's', '\r', '\n', '\r', '\n'})
	require.NoError(t, err)
	read := make(chan []byte)
	go func() {
		b, _ := io.ReadAll(conn)
		read <- b
	}()
	assert.NotContains(t, string(testutil.Recv(t, read, "the connection to close")), "HTTP/")

	resp, err := http.Get("http://" + addr + "/")
	require.NoError(t, err)
	assert.Equal(t, "tls=false", body(t, resp))
}
