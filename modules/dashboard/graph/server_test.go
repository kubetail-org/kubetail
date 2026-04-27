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
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/modules/shared/testutils"

	"github.com/kubetail-org/kubetail/modules/dashboard/pkg/config"
)

const testCSRFToken = "expected-csrf-token"

// withSessionCSRFToken stands in for the gin middleware that injects the
// session's expected CSRF token into the request context.
func withSessionCSRFToken(h http.Handler, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), SessionCSRFTokenCtxKey, token)
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

func TestServerDrainWithContext_NoConnections(t *testing.T) {
	cfg := config.DefaultConfig()
	s := NewServer(cfg, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := s.DrainWithContext(ctx)
	require.NoError(t, err)
}

func TestServerWebSocketCheckOrigin(t *testing.T) {
	cfg := config.DefaultConfig()
	s := NewServer(cfg, nil)

	client := testutils.NewWebTestClient(t, s)
	defer client.Teardown()

	wsURL := "ws" + strings.TrimPrefix(client.Server.URL, "http") + "/graphql"
	httpHost := strings.TrimPrefix(client.Server.URL, "http://")

	tests := []struct {
		name       string
		setOrigin  string
		wantStatus int
	}{
		{
			"no Origin is rejected",
			"",
			http.StatusForbidden,
		},
		{
			"same-origin Origin is accepted",
			"http://" + httpHost,
			http.StatusSwitchingProtocols,
		},
		{
			"cross-origin Origin is rejected",
			"https://evil.example.com",
			http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header := http.Header{}
			if tt.setOrigin != "" {
				header.Set("Origin", tt.setOrigin)
			}
			_, resp, _ := websocket.DefaultDialer.Dial(wsURL, header)
			require.Equal(t, tt.wantStatus, resp.StatusCode)
		})
	}
}

func TestServerWebSocketCSRFInit(t *testing.T) {
	cfg := config.DefaultConfig()
	s := NewServer(cfg, nil)

	client := testutils.NewWebTestClient(t, withSessionCSRFToken(s, testCSRFToken))
	defer client.Teardown()

	wsURL := "ws" + strings.TrimPrefix(client.Server.URL, "http") + "/graphql"
	dialer := websocket.Dialer{Subprotocols: []string{"graphql-transport-ws"}}

	tests := []struct {
		name    string
		payload map[string]any
		wantAck bool
	}{
		{
			name:    "missing csrfToken is rejected",
			payload: map[string]any{"type": "connection_init"},
		},
		{
			name:    "wrong csrfToken is rejected",
			payload: map[string]any{"type": "connection_init", "payload": map[string]any{"csrfToken": "bad"}},
		},
		{
			name:    "matching csrfToken is accepted",
			payload: map[string]any{"type": "connection_init", "payload": map[string]any{"csrfToken": testCSRFToken}},
			wantAck: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, _, err := dialer.Dial(wsURL, http.Header{"Origin": []string{client.Server.URL}})
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
			// Reject path: server may emit connection_error then close, or close directly — either way no ack.
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
	cfg := config.DefaultConfig()
	s := NewServer(cfg, nil)

	client := testutils.NewWebTestClient(t, s)
	defer client.Teardown()

	wsURL := "ws" + strings.TrimPrefix(client.Server.URL, "http") + "/graphql"
	dialer := websocket.Dialer{EnableCompression: true}
	conn, resp, err := dialer.Dial(wsURL, http.Header{"Origin": []string{client.Server.URL}})
	require.NoError(t, err)
	defer conn.Close()

	require.Empty(t, resp.Header.Get("Sec-WebSocket-Extensions"))
}

func TestServerDrainWithContext_CancelledContext(t *testing.T) {
	cfg := config.DefaultConfig()
	s := NewServer(cfg, nil)

	// Simulate an open connection that never finishes
	s.wg.Add(1)
	defer s.wg.Done()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := s.DrainWithContext(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestServerDrainWithContext_DeadlineExceeded(t *testing.T) {
	cfg := config.DefaultConfig()
	s := NewServer(cfg, nil)

	// Simulate an open connection that never finishes
	s.wg.Add(1)
	defer s.wg.Done()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := s.DrainWithContext(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestServerDrainWithContext_WaitsForHTTPRequests(t *testing.T) {
	cfg := config.DefaultConfig()
	s := NewServer(cfg, nil)

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
	cfg := config.DefaultConfig()

	s := NewServer(cfg, nil)

	client := testutils.NewWebTestClient(t, withSessionCSRFToken(s, testCSRFToken))
	defer client.Teardown()

	// Dial WebSocket with graphql-transport-ws subprotocol
	wsURL := "ws" + strings.TrimPrefix(client.Server.URL, "http") + "/graphql"
	dialer := websocket.Dialer{Subprotocols: []string{"graphql-transport-ws"}}
	conn, _, err := dialer.Dial(wsURL, http.Header{"Origin": []string{client.Server.URL}})
	require.NoError(t, err)
	defer conn.Close()

	// Complete the graphql-transport-ws handshake
	err = conn.WriteJSON(map[string]any{"type": "connection_init", "payload": map[string]any{"csrfToken": testCSRFToken}})
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
	cfg := config.DefaultConfig()

	s := NewServer(cfg, nil)

	client := testutils.NewWebTestClient(t, withSessionCSRFToken(s, testCSRFToken))
	defer client.Teardown()

	wsURL := "ws" + strings.TrimPrefix(client.Server.URL, "http") + "/graphql"
	dialer := websocket.Dialer{Subprotocols: []string{"graphql-transport-ws"}}

	const numConns = 3
	conns := make([]*websocket.Conn, numConns)

	// Open multiple WebSocket connections
	for i := range numConns {
		conn, _, err := dialer.Dial(wsURL, http.Header{"Origin": []string{client.Server.URL}})
		require.NoError(t, err)
		defer conn.Close()

		err = conn.WriteJSON(map[string]any{"type": "connection_init", "payload": map[string]any{"csrfToken": testCSRFToken}})
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
