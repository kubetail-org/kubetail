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
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/suite"
	"k8s.io/utils/pointer"

	"github.com/kubetail-org/kubetail/internal/k8shelpers/mock"
)

type AuthTestSuite struct {
	WebTestSuiteBase
}

func (suite *AuthTestSuite) SetupTest() {
	// reset mock
	suite.App.k8sHelperService = &mock.Service{}
}

func (suite *AuthTestSuite) TestLoginPOSTFormErrors() {
	// init empty form
	form := url.Values{}

	// make request
	resp := suite.PostForm("/api/auth/login", form)

	// check result
	suite.Equal(http.StatusUnprocessableEntity, resp.StatusCode)
	suite.Contains(string(resp.Body), "Please enter your token")
}

func (suite *AuthTestSuite) TestLoginPOSTSuccess() {
	// configure mock
	svc := suite.App.k8sHelperService.(*mock.Service)
	svc.On("HasAccess", "xxx").Return("user", nil)

	// execute request
	form := url.Values{}
	form.Add("token", "xxx")
	resp := suite.PostForm("/api/auth/login", form, func(c *gin.Context) {
		// execute handler
		c.Next()

		// check that token was added to session
		session := sessions.Default(c)
		token, ok := session.Get(k8sTokenCtxKey).(string)
		suite.True(ok)
		suite.Equal("xxx", token)
	})

	// assertions
	svc.AssertNumberOfCalls(suite.T(), "HasAccess", 1)
	svc.AssertCalled(suite.T(), "HasAccess", "xxx")
	suite.Equal(http.StatusNoContent, resp.StatusCode)
}

func (suite *AuthTestSuite) TestLoginPOSTFailure() {
	// configure mock
	svc := suite.App.k8sHelperService.(*mock.Service)
	svc.On("HasAccess", "xxx").Return("", errors.New(""))

	// execute request
	form := url.Values{}
	form.Add("token", "xxx")
	resp := suite.PostForm("/api/auth/login", form)

	// assertions
	svc.AssertNumberOfCalls(suite.T(), "HasAccess", 1)
	svc.AssertCalled(suite.T(), "HasAccess", "xxx")
	suite.Equal(http.StatusUnprocessableEntity, resp.StatusCode)
}

func (suite *AuthTestSuite) TestLogoutPOST() {
	// configure mock
	svc := suite.App.k8sHelperService.(*mock.Service)
	svc.On("HasAccess", "xxx").Return("user", nil)

	// log in with a cookie-enabled client
	client := suite.NewClient()

	// login
	form := url.Values{}
	form.Add("token", "xxx")
	resp1 := client.PostForm("/api/auth/login", form)

	// verify that session cookie was added
	cookie1 := GetCookie(resp1.Cookies, "session")
	suite.NotNil(cookie1)

	// logout
	resp2 := client.PostForm("/api/auth/logout", nil, func(c *gin.Context) {
		// execute handler
		c.Next()

		// check session
		session := sessions.Default(c)
		suite.Nil(session.Get(k8sTokenSessionKey))
	})

	// verify session cookie was changed
	cookie2 := GetCookie(resp2.Cookies, "session")
	suite.NotNil(cookie2)
	suite.NotEqual(cookie1.Value, cookie2.Value)
}

func (suite *AuthTestSuite) TestSessionGET() {
	type Session struct {
		User      *string
		Timestamp string
	}

	tests := []struct {
		name              string
		setAuthMode       AuthMode
		wantLoggedOutUser *string
		wantLoggedInUser  *string
	}{
		{"cluster", AuthModeCluster, pointer.String("cluster"), pointer.String("cluster")},
		{"local", AuthModeLocal, pointer.String("local"), pointer.String("local")},
		{"token", AuthModeToken, nil, pointer.String("user")},
	}

	for _, tt := range tests {
		suite.Run(tt.name, func() {

			// init app
			cfg := NewTestConfig()
			cfg.AuthMode = tt.setAuthMode
			app := NewTestApp(cfg)

			// configure mock
			svc := &mock.Service{}
			svc.On("HasAccess", "xxx").Return("user", nil)
			app.k8sHelperService = svc

			// init client
			client := NewWebTestClient(suite.T(), app)

			// logged-out tests
			{
				// get session
				resp := client.Get("/api/auth/session")

				// parse json
				var session Session
				err := json.Unmarshal(resp.Body, &session)
				suite.Nil(err)

				// check user
				suite.Equal(tt.wantLoggedOutUser, session.User)

				// check timestamp
				_, err = time.Parse(time.RFC3339Nano, session.Timestamp)
				suite.Nil(err)
			}

			// logged-in tests
			{
				// log in
				form := url.Values{}
				form.Add("token", "xxx")
				client.PostForm("/api/auth/login", form)

				// get session
				resp := client.Get("/api/auth/session")

				// parse json
				var session Session
				err := json.Unmarshal(resp.Body, &session)
				suite.Nil(err)

				// check user
				suite.Equal(tt.wantLoggedInUser, session.User)

				// check timestamp
				_, err = time.Parse(time.RFC3339Nano, session.Timestamp)
				suite.Nil(err)
			}

			client.Teardown()
		})
	}
}

// test runner
func TestAuthHandlers(t *testing.T) {
	suite.Run(t, new(AuthTestSuite))
}
