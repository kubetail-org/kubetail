package app

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/grpc/authpb"
)

// h2cGRPCClient dials the httptest server with insecure credentials — i.e. h2c
// (HTTP/2 prior knowledge, no TLS), the same wire the Rust tonic client uses
// over the UDS — and returns an AuthService client.
func h2cGRPCClient(t *testing.T, ts *httptest.Server) authpb.AuthServiceClient {
	t.Helper()
	conn, err := grpc.NewClient(strings.TrimPrefix(ts.URL, "http://"), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return authpb.NewAuthServiceClient(conn)
}

// serveApp serves a over httptest with the Protocols main serves it with.
func serveApp(t *testing.T, a *App) *httptest.Server {
	t.Helper()
	ts := httptest.NewUnstartedServer(a)
	ts.Config.Protocols = Protocols()
	ts.Start()
	t.Cleanup(ts.Close)
	return ts
}

// TestH2C_SSEStillStreams confirms SSE subscriptions keep streaming frames
// incrementally over HTTP/1.1 on the server that also speaks h2c (the
// ResponseWriter stays Flusher-backed). Guards against a buffering regression.
func TestH2C_SSEStillStreams(t *testing.T) {
	a, _ := newTestApp(t)
	ts := serveApp(t, a)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/graphql", strings.NewReader(`{"query":"subscription { authStateWatch { authenticated } }"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// The first non-comment event must arrive well before the stream would
	// complete — proving frames flush incrementally, not buffered to the end.
	sc := bufio.NewScanner(resp.Body)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if line := sc.Text(); strings.HasPrefix(line, "event: next") {
			return
		}
		if !sc.Scan() {
			break
		}
	}
	t.Fatal("did not observe an incremental SSE `event: next` frame")
}

// TestH2C_GRPCAuthState exercises the gRPC surface over h2c on the shared
// listener: AuthStateWatch yields a snapshot (signed-out in a standalone/test
// run), proving server-streaming gRPC rides the same socket as GraphQL.
func TestH2C_GRPCAuthState(t *testing.T) {
	a, _ := newTestApp(t)
	ts := serveApp(t, a)
	client := h2cGRPCClient(t, ts)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.AuthStateWatch(ctx, &authpb.AuthStateWatchRequest{})
	require.NoError(t, err)

	snap, err := stream.Recv()
	require.NoError(t, err)
	assert.False(t, snap.GetAuthenticated())
	assert.Nil(t, snap.GetIdentity())
}
