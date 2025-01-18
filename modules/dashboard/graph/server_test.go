// Copyright 2024-2025 Andres Morey
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
	"net/http"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"

	"github.com/kubetail-org/kubetail/modules/shared/config"
	"github.com/kubetail-org/kubetail/modules/shared/testutils"
)

func TestServer(t *testing.T) {
	t.Run("cross-origin websocket requests are allowed when csrf protection is disabled", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Dashboard.Environment = config.EnvironmentCluster

		graphqlServer, err := NewServer(cfg, nil, nil)
		assert.Nil(t, err)

		client := testutils.NewWebTestClient(t, graphqlServer)
		defer client.Teardown()

		// init websocket connection
		u := "ws" + strings.TrimPrefix(client.Server.URL, "http") + "/graphql"
		h := http.Header{}
		conn, resp, err := websocket.DefaultDialer.Dial(u, h)

		// check that response was ok
		assert.Nil(t, err)
		assert.NotNil(t, conn)
		assert.Equal(t, 101, resp.StatusCode)
		defer conn.Close()

		// write
		conn.WriteJSON(map[string]string{"type": "connection_init"})

		// read
		_, msg, err := conn.ReadMessage()
		assert.Nil(t, err)
		assert.Contains(t, string(msg), "connection_ack")
	})

	t.Run("websocket requests require csrf validation when csrf protection is enabled", func(t *testing.T) {
		csrfProtect := func(h http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "nope", http.StatusUnauthorized)
			})
		}

		cfg := config.DefaultConfig()
		cfg.Dashboard.Environment = config.EnvironmentCluster

		graphqlServer, err := NewServer(cfg, nil, csrfProtect)
		assert.Nil(t, err)

		client := testutils.NewWebTestClient(t, graphqlServer)
		defer client.Teardown()

		// init websocket connection
		u := "ws" + strings.TrimPrefix(client.Server.URL, "http") + "/graphql"
		h := http.Header{}
		conn, resp, err := websocket.DefaultDialer.Dial(u, h)

		// check that response was ok
		assert.Nil(t, err)
		assert.NotNil(t, conn)
		assert.Equal(t, 101, resp.StatusCode)
		defer conn.Close()

		// write
		conn.WriteJSON(map[string]string{"type": "connection_init"})

		// read
		_, msg, err := conn.ReadMessage()
		assert.Nil(t, err)
		assert.Contains(t, string(msg), "connection_error")
	})
}
