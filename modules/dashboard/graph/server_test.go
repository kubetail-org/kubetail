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
	"net/http"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	sharedcfg "github.com/kubetail-org/kubetail/modules/shared/config"
	"github.com/kubetail-org/kubetail/modules/shared/testutils"

	"github.com/kubetail-org/kubetail/modules/dashboard/pkg/config"
)

func TestServer(t *testing.T) {
	tests := []struct {
		name           string
		setCsrfEnabled bool
		setHeader      http.Header
		wantStatus     int
	}{
		{
			"csrf disabled, non-browser client",
			false,
			http.Header{},
			http.StatusSwitchingProtocols,
		},
		{
			"csrf disabled, same-origin request",
			false,
			http.Header{"Sec-Fetch-Site": []string{"same-origin"}},
			http.StatusSwitchingProtocols,
		},
		{
			"csrf disabled, cross-site request",
			false,
			http.Header{"Sec-Fetch-Site": []string{"cross-site"}},
			http.StatusSwitchingProtocols,
		},
		{
			"csrf enabled, non-browser client",
			true,
			http.Header{},
			http.StatusSwitchingProtocols,
		},
		{
			"csrf enabled, same-origin request",
			true,
			http.Header{"Sec-Fetch-Site": []string{"same-origin"}},
			http.StatusSwitchingProtocols,
		},
		{
			"csrf enabled, cross-site request",
			true,
			http.Header{"Sec-Fetch-Site": []string{"cross-site"}},
			http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Environment = sharedcfg.EnvironmentCluster
			cfg.CSRF.Enabled = tt.setCsrfEnabled

			graphqlServer := NewServer(cfg, nil)

			client := testutils.NewWebTestClient(t, graphqlServer)
			defer client.Teardown()

			// init websocket connection
			u := "ws" + strings.TrimPrefix(client.Server.URL, "http") + "/graphql"
			_, resp, _ := websocket.DefaultDialer.Dial(u, tt.setHeader)

			// check status code
			require.Equal(t, tt.wantStatus, resp.StatusCode)
		})
	}
}
