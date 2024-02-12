// Copyright 2024 Andres Morey
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

package ginapp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/suite"
)

type GraphQLTestSuite struct {
	WebTestSuiteBase
}

func (suite *GraphQLTestSuite) TestAccess() {
	suite.Run("GraphQL Endpoint", func() {
		schemaQuery := `{"query":"{ __schema { types { name } } }"}`

		suite.Run("simple POST requests are rejected", func() {
			// build request
			client := suite.defaultclient
			req := client.NewRequest("POST", "/graphql", strings.NewReader(schemaQuery))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

			// execute request
			resp := client.Do(req)

			// check response
			suite.Equal(http.StatusBadRequest, resp.StatusCode)
		})

		suite.Run("preflighted POST requests are ok", func() {
			// build request
			client := suite.defaultclient
			req := client.NewRequest("POST", "/graphql", strings.NewReader(schemaQuery))
			req.Header.Set("Content-Type", "application/json")

			// execute request
			resp := client.Do(req)

			// check response
			suite.Equal(http.StatusOK, resp.StatusCode)
		})

		suite.Run("GET requests are rejected", func() {
			// build request
			client := suite.defaultclient
			req := client.NewRequest("GET", "/graphql", strings.NewReader(schemaQuery))
			req.Header.Set("Content-Type", "application/json")

			// execute request
			resp := client.Do(req)

			// check response
			suite.Equal(http.StatusUnprocessableEntity, resp.StatusCode)
			suite.Contains(string(resp.Body), "no operation provided")
		})

		suite.Run("DELETE requests are ignored", func() {
			// build request
			client := suite.defaultclient
			req := client.NewRequest("DELETE", "/graphql", strings.NewReader(schemaQuery))
			req.Header.Set("Content-Type", "application/json")

			// execute request
			resp := client.Do(req)

			// check response
			suite.Equal(http.StatusNotFound, resp.StatusCode)
		})

		suite.Run("OPTIONS requests are ignored", func() {
			// build request
			client := suite.defaultclient
			req := client.NewRequest("OPTIONS", "/graphql", nil)
			req.Header.Set("Content-Type", "application/json")

			// execute request
			resp := client.Do(req)

			// check response
			suite.Equal(http.StatusNotFound, resp.StatusCode)
		})

		suite.Run("cross-origin subscriptions aren't allowed", func() {
			// init websocket connection
			u := "ws" + strings.TrimPrefix(suite.defaultclient.testserver.URL, "http") + "/graphql"
			h := http.Header{}
			h.Add("Origin", "not-the-host.com")
			_, _, err := websocket.DefaultDialer.Dial(u, h)

			// check response
			suite.NotNil(err)
		})
	})
}

func (suite *GraphQLTestSuite) TestAuth() {
	tests := []struct {
		name     string
		mode     AuthMode
		wantCode int
	}{
		{
			"local",
			AuthModeLocal,
			http.StatusUnprocessableEntity,
		},
		{
			"cluster",
			AuthModeCluster,
			http.StatusUnprocessableEntity,
		},
		{
			"token",
			AuthModeToken,
			http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		suite.Run(tt.name, func() {
			cfg := NewTestConfig()
			cfg.AuthMode = tt.mode
			app := NewTestApp(cfg)

			// request without token
			w1 := httptest.NewRecorder()
			r1 := httptest.NewRequest("GET", "/graphql", nil)
			app.ServeHTTP(w1, r1)
			suite.Equal(tt.wantCode, w1.Result().StatusCode)

			// request with token
			w2 := httptest.NewRecorder()
			r2 := httptest.NewRequest("GET", "/graphql", nil)
			r2.Header.Set("Authorization", "Bearer xxx")
			app.ServeHTTP(w2, r2)
			suite.Equal(http.StatusUnprocessableEntity, w2.Result().StatusCode)
		})
	}
}

// test runner
func TestGraphQLHandlers(t *testing.T) {
	suite.Run(t, new(GraphQLTestSuite))
}
