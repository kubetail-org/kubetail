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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	authv1 "k8s.io/api/authentication/v1"
	"k8s.io/utils/ptr"

	"github.com/kubetail-org/kubetail/modules/dashboard/pkg/config"
	"github.com/kubetail-org/kubetail/modules/shared/testutils"
)

type authTestSuite struct {
	suite.Suite
	app    *App
	client *testutils.WebTestClient
}

// test runner
func TestAuthHandlers(t *testing.T) {
	suite.Run(t, new(authTestSuite))
}

func (suite *authTestSuite) SetupTest() {
	// Init app
	app := newTestApp(nil)
	app.queryHelpers = &mockQueryHelpers{}

	// Init client
	client := testutils.NewWebTestClient(suite.T(), app)

	// Prime the CSRF token so that subsequent unsafe-method requests include it.
	client.Get("/api/auth/session")

	// Save
	suite.app = app
	suite.client = client
}

func (suite *authTestSuite) TearDownTest() {
	suite.client.Teardown()
}

func (suite *authTestSuite) TestLoginPOSTFormErrors() {
	// Init empty form
	form := url.Values{}

	// Make request
	resp := suite.client.PostForm("/api/auth/login", form)

	// check result
	suite.Equal(http.StatusUnprocessableEntity, resp.StatusCode)
	suite.Contains(string(resp.Body), "Please enter your token")

	// check result
	suite.Equal(http.StatusUnprocessableEntity, resp.StatusCode)
	suite.Contains(string(resp.Body), "Please enter your token")
}

// Behavioral check: an unsafe-method auth route is gated by the CSRF
// middleware. Mirrors the protected-route behavioral coverage in download_test.go.
func TestLoginPOSTRequiresCSRFToken(t *testing.T) {
	app := newTestApp(nil)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/auth/login", nil)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	app.ServeHTTP(w, r)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func (suite *authTestSuite) TestLoginPOSTSuccess() {
	// Configure mock
	m := suite.app.queryHelpers.(*mockQueryHelpers)
	m.On("HasAccess", mock.Anything, "xxx").Return(&authv1.TokenReview{
		Status: authv1.TokenReviewStatus{
			Authenticated: true,
		},
	}, nil)

	// Init form
	form := url.Values{}
	form.Add("token", "xxx")

	// Execute
	resp := suite.client.PostForm("/api/auth/login", form)

	// Assertions
	m.AssertNumberOfCalls(suite.T(), "HasAccess", 1)
	m.AssertCalled(suite.T(), "HasAccess", mock.Anything, "xxx")
	suite.Equal(http.StatusNoContent, resp.StatusCode)
}

func (suite *authTestSuite) TestLoginPOSTFailure() {
	// Configure mock
	m := suite.app.queryHelpers.(*mockQueryHelpers)
	m.On("HasAccess", mock.Anything, "xxx").Return(&authv1.TokenReview{
		Status: authv1.TokenReviewStatus{
			Authenticated: false,
		},
	}, nil)

	// Init form
	form := url.Values{}
	form.Add("token", "xxx")

	// Execute
	resp := suite.client.PostForm("/api/auth/login", form)

	// Assertions
	m.AssertNumberOfCalls(suite.T(), "HasAccess", 1)
	m.AssertCalled(suite.T(), "HasAccess", mock.Anything, "xxx")
	suite.Equal(http.StatusUnprocessableEntity, resp.StatusCode)
}

func (suite *authTestSuite) TestLogoutPOSTSuccess() {
	m := suite.app.queryHelpers.(*mockQueryHelpers)
	m.On("HasAccess", mock.Anything, "xxx").Return(&authv1.TokenReview{
		Status: authv1.TokenReviewStatus{
			Authenticated: true,
		},
	}, nil)

	// Init form
	form := url.Values{}
	form.Add("token", "xxx")

	// Log in
	resp1 := suite.client.PostForm("/api/auth/login", form)

	// Verify that session cookie was added
	cookie1 := getCookie(resp1.Cookies, "session")
	suite.NotNil(cookie1)

	// Log out
	resp2 := suite.client.PostForm("/api/auth/logout", nil)

	// Verify session cookie was changed
	cookie2 := getCookie(resp2.Cookies, "session")
	suite.NotNil(cookie2)
	suite.NotEqual(cookie1.Value, cookie2.Value)
}

func (suite *authTestSuite) TestSessionGET() {
	type Session struct {
		User      *string
		Timestamp string
	}

	tests := []struct {
		name              string
		setAuthMode       config.AuthMode
		wantLoggedOutUser *string
		wantLoggedInUser  *string
	}{
		{"auto", config.AuthModeAuto, ptr.To("auto"), ptr.To("auto")},
		{"token", config.AuthModeToken, nil, ptr.To("user-xxx")},
	}

	for _, tt := range tests {
		suite.Run(tt.name, func() {
			// Init config
			cfg := newTestConfig()
			cfg.AuthMode = tt.setAuthMode

			// Init app
			app := newTestApp(cfg)

			// Configure mock
			m := new(mockQueryHelpers)
			m.On("HasAccess", mock.Anything, "xxx").Return(&authv1.TokenReview{
				Status: authv1.TokenReviewStatus{
					Authenticated: true,
					User: authv1.UserInfo{
						Username: "user-xxx",
					},
				},
			}, nil)
			app.queryHelpers = m

			// Init client
			client := testutils.NewWebTestClient(suite.T(), app)

			// Logged-out tests
			{
				// Get session
				resp := client.Get("/api/auth/session")

				// Parse json
				var session Session
				err := json.Unmarshal(resp.Body, &session)
				suite.Nil(err)

				// Check user
				suite.Equal(tt.wantLoggedOutUser, session.User)

				// Check timestamp
				_, err = time.Parse(time.RFC3339Nano, session.Timestamp)
				suite.Nil(err)
			}

			// Logged-in tests
			{
				// Log in
				form := url.Values{}
				form.Add("token", "xxx")
				client.PostForm("/api/auth/login", form)

				// Get session
				resp := client.Get("/api/auth/session")

				// Parse json
				var session Session
				err := json.Unmarshal(resp.Body, &session)
				suite.Nil(err)

				// Check user
				suite.Equal(tt.wantLoggedInUser, session.User)

				// Check timestamp
				_, err = time.Parse(time.RFC3339Nano, session.Timestamp)
				suite.Nil(err)
			}

			client.Teardown()
		})
	}
}

func TestSessionGETCSRFToken(t *testing.T) {
	app := newTestApp(nil)

	t.Run("GET /api/auth/session returns non-empty X-CSRF-Token header", func(t *testing.T) {
		client := testutils.NewWebTestClient(t, app)
		defer client.Teardown()
		resp := client.Get("/api/auth/session")
		assert.NotEmpty(t, resp.Header.Get("X-CSRF-Token"))
	})

	t.Run("repeated GET /api/auth/session returns the same token", func(t *testing.T) {
		client := testutils.NewWebTestClient(t, app)
		defer client.Teardown()
		resp1 := client.Get("/api/auth/session")
		resp2 := client.Get("/api/auth/session")
		token1 := resp1.Header.Get("X-CSRF-Token")
		require.NotEmpty(t, token1)
		assert.Equal(t, token1, resp2.Header.Get("X-CSRF-Token"))
	})
}
