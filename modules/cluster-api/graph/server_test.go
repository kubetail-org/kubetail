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

package graph

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/modules/shared/testutils"
)

func TestServerSSETransportServesQueries(t *testing.T) {
	s := NewServer(nil, nil, []string{})

	client := testutils.NewWebTestClient(t, s)
	defer client.Teardown()

	req := client.NewRequest("POST", "/graphql", strings.NewReader(`{"query":"{ __typename }"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp := client.Do(req)

	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/event-stream")

	body := string(resp.Body)
	assert.Contains(t, body, "event: next", "expected SSE next event")
	assert.Contains(t, body, `"__typename":"Query"`, "expected query result in next event payload")
	assert.Contains(t, body, "event: complete", "expected SSE complete event")
}

func TestServerWebSocketCheckOrigin(t *testing.T) {
	tests := []struct {
		name       string
		setHeader  http.Header
		wantStatus int
	}{
		{
			"bot client (no Origin) is accepted",
			http.Header{},
			http.StatusSwitchingProtocols,
		},
		{
			"browser client (Origin set) is rejected",
			http.Header{"Origin": []string{"https://evil.example.com"}},
			http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			graphqlServer := NewServer(nil, nil, []string{})

			client := testutils.NewWebTestClient(t, graphqlServer)
			defer client.Teardown()

			u := "ws" + strings.TrimPrefix(client.Server.URL, "http") + "/graphql"
			_, resp, _ := websocket.DefaultDialer.Dial(u, tt.setHeader)

			require.Equal(t, tt.wantStatus, resp.StatusCode)
		})
	}
}

// withSessionCSRFToken stands in for the cluster-api middleware that copies
// X-Forwarded-CSRF-Token from the upgrade request into the request context.
func withSessionCSRFToken(h http.Handler, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), SessionCSRFTokenCtxKey, token)
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

func TestServerWebSocketCSRFInit(t *testing.T) {
	const expected = "expected-csrf-token"

	tests := []struct {
		name    string
		ctxTok  string         // "" = bot path, no token in ctx
		payload map[string]any // connection_init payload
		wantAck bool
	}{
		{
			name:    "bot path (no header) accepts empty payload",
			payload: map[string]any{"type": "connection_init"},
			wantAck: true,
		},
		{
			name:    "bot path (no header) accepts any csrfToken",
			payload: map[string]any{"type": "connection_init", "payload": map[string]any{"csrfToken": "anything"}},
			wantAck: true,
		},
		{
			name:    "browser path with matching csrfToken is accepted",
			ctxTok:  expected,
			payload: map[string]any{"type": "connection_init", "payload": map[string]any{"csrfToken": expected}},
			wantAck: true,
		},
		{
			name:    "browser path with wrong csrfToken is rejected",
			ctxTok:  expected,
			payload: map[string]any{"type": "connection_init", "payload": map[string]any{"csrfToken": "bad"}},
		},
		{
			name:    "browser path with missing csrfToken is rejected",
			ctxTok:  expected,
			payload: map[string]any{"type": "connection_init"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewServer(nil, nil, []string{})
			var handler http.Handler = s
			if tt.ctxTok != "" {
				handler = withSessionCSRFToken(s, tt.ctxTok)
			}

			client := testutils.NewWebTestClient(t, handler)
			defer client.Teardown()

			wsURL := "ws" + strings.TrimPrefix(client.Server.URL, "http") + "/graphql"
			dialer := websocket.Dialer{Subprotocols: []string{"graphql-transport-ws"}}
			conn, _, err := dialer.Dial(wsURL, nil)
			require.NoError(t, err)
			defer conn.Close()

			require.NoError(t, conn.WriteJSON(tt.payload))

			conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			var msg map[string]any
			err = conn.ReadJSON(&msg)
			if tt.wantAck {
				require.NoError(t, err)
				require.Equal(t, "connection_ack", msg["type"])
				return
			}
			if err == nil {
				require.NotEqual(t, "connection_ack", msg["type"])
				conn.SetReadDeadline(time.Now().Add(2 * time.Second))
				_, _, err = conn.ReadMessage()
			}
			require.Error(t, err)
		})
	}
}

func TestServerWebSocketCompressionDisabled(t *testing.T) {
	graphqlServer := NewServer(nil, nil, []string{})

	client := testutils.NewWebTestClient(t, graphqlServer)
	defer client.Teardown()

	wsURL := "ws" + strings.TrimPrefix(client.Server.URL, "http") + "/graphql"
	dialer := websocket.Dialer{EnableCompression: true}
	conn, resp, err := dialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	require.Empty(t, resp.Header.Get("Sec-WebSocket-Extensions"))
}

func TestServerDrainWithContext_NoConnections(t *testing.T) {
	s := NewServer(nil, nil, []string{})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := s.DrainWithContext(ctx)
	require.NoError(t, err)
}

func TestServerDrainWithContext_CancelledContext(t *testing.T) {
	s := NewServer(nil, nil, []string{})

	// Simulate an open connection that never finishes
	s.wg.Add(1)
	defer s.wg.Done()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := s.DrainWithContext(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestServerDrainWithContext_DeadlineExceeded(t *testing.T) {
	s := NewServer(nil, nil, []string{})

	// Simulate an open connection that never finishes
	s.wg.Add(1)
	defer s.wg.Done()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := s.DrainWithContext(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestServerDrainWithContext_WaitsForHTTPRequests(t *testing.T) {
	s := NewServer(nil, nil, []string{})

	client := testutils.NewWebTestClient(t, s)
	defer client.Teardown()

	// Send a POST request with a valid GraphQL query — the server will process it
	// and the wg counter will be held for the duration of the request.
	// Send a synchronous request; once it returns the wg counter must be back to zero
	resp, err := http.Post(client.Server.URL+"/graphql", "application/json", strings.NewReader(`{"query":"{ __typename }"}`))
	require.NoError(t, err)
	resp.Body.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, s.DrainWithContext(ctx))
}

func TestServerNotifyShutdown_ClosesConnections(t *testing.T) {
	s := NewServer(nil, nil, []string{})

	client := testutils.NewWebTestClient(t, s)
	defer client.Teardown()

	// Dial WebSocket with graphql-transport-ws subprotocol
	wsURL := "ws" + strings.TrimPrefix(client.Server.URL, "http") + "/graphql"
	dialer := websocket.Dialer{Subprotocols: []string{"graphql-transport-ws"}}
	conn, _, err := dialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	// Complete the graphql-transport-ws handshake
	err = conn.WriteJSON(map[string]any{"type": "connection_init"})
	require.NoError(t, err)

	// Read connection_ack — confirms the WebSocket is fully established
	var msg map[string]any
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	err = conn.ReadJSON(&msg)
	require.NoError(t, err)
	require.Equal(t, "connection_ack", msg["type"])

	// Signal shutdown
	s.NotifyShutdown()

	// Connection should be closed by the server
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, readErr := conn.ReadMessage()
	require.Error(t, readErr)

	// All connections should be drained
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, s.DrainWithContext(ctx))
}

func TestServerNotifyShutdown_ClosesMultipleConnections(t *testing.T) {
	s := NewServer(nil, nil, []string{})

	client := testutils.NewWebTestClient(t, s)
	defer client.Teardown()

	wsURL := "ws" + strings.TrimPrefix(client.Server.URL, "http") + "/graphql"
	dialer := websocket.Dialer{Subprotocols: []string{"graphql-transport-ws"}}

	const numConns = 3
	conns := make([]*websocket.Conn, numConns)

	// Open multiple WebSocket connections
	for i := range numConns {
		conn, _, err := dialer.Dial(wsURL, nil)
		require.NoError(t, err)
		defer conn.Close()

		err = conn.WriteJSON(map[string]any{"type": "connection_init"})
		require.NoError(t, err)

		var msg map[string]any
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		err = conn.ReadJSON(&msg)
		require.NoError(t, err)
		require.Equal(t, "connection_ack", msg["type"])

		conns[i] = conn
	}

	// Signal shutdown
	s.NotifyShutdown()

	// All connections should be closed by the server
	for i, conn := range conns {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, _, readErr := conn.ReadMessage()
		require.Error(t, readErr, "connection %d should be closed", i)
	}

	// All connections should be drained
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, s.DrainWithContext(ctx))
}
