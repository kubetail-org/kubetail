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

package wstransport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/99designs/gqlgen/graphql/handler/testserver"
	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func dial(t *testing.T, checkOrigin func(r *http.Request) bool, origin string) *http.Response {
	t.Helper()

	h := testserver.New()
	h.AddTransport(Websocket{CheckOrigin: checkOrigin})
	srv := httptest.NewServer(h)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	header := http.Header{}
	if origin != "" {
		header.Set("Origin", origin)
	}
	conn, resp, _ := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), &websocket.DialOptions{
		HTTPHeader:   header,
		Subprotocols: []string{"graphql-transport-ws"},
	})
	if conn != nil {
		conn.CloseNow()
	}
	require.NotNil(t, resp)
	return resp
}

func TestWebsocket_RejectedOriginIsForbidden(t *testing.T) {
	resp := dial(t, func(*http.Request) bool { return false }, "")
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestWebsocket_CheckOriginOverridesSameHostCheck(t *testing.T) {
	var gotOrigin string
	resp := dial(t, func(r *http.Request) bool {
		gotOrigin = r.Header.Get("Origin")
		return true
	}, "https://elsewhere.example.com")
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
	require.Equal(t, "https://elsewhere.example.com", gotOrigin)
}
