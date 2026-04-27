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

package app

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/modules/shared/testutils"
)

func TestWebSocketCSRFToken(t *testing.T) {
	app := newTestApp(nil)

	tests := []struct {
		name      string
		token     func(captured string) any // returns csrfToken value to send (nil = omit)
		wantAck   bool
		wantClose bool
	}{
		{
			name:      "missing csrfToken is rejected",
			token:     func(string) any { return nil },
			wantClose: true,
		},
		{
			name:      "wrong csrfToken is rejected",
			token:     func(string) any { return "bad" },
			wantClose: true,
		},
		{
			name:    "matching csrfToken is accepted",
			token:   func(captured string) any { return captured },
			wantAck: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := testutils.NewWebTestClient(t, app)
			defer client.Teardown()

			// Establish session and capture CSRF token
			resp := client.Get("/api/auth/session")
			captured := resp.Header.Get("X-CSRF-Token")
			require.NotEmpty(t, captured)

			wsURL := "ws" + strings.TrimPrefix(client.Server.URL, "http") + "/graphql"
			dialer := websocket.Dialer{
				Subprotocols: []string{"graphql-transport-ws"},
				Jar:          client.Server.Client().Jar,
			}
			conn, _, err := dialer.Dial(wsURL, http.Header{"Origin": []string{client.Server.URL}})
			require.NoError(t, err)
			defer conn.Close()

			initMsg := map[string]any{"type": "connection_init"}
			if tok := tt.token(captured); tok != nil {
				initMsg["payload"] = map[string]any{"csrfToken": tok}
			}
			require.NoError(t, conn.WriteJSON(initMsg))

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
