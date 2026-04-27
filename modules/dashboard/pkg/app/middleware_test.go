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
	"net/http/httptest"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/kubetail-org/kubetail/modules/dashboard/graph"
	"github.com/kubetail-org/kubetail/modules/dashboard/pkg/config"
	"github.com/kubetail-org/kubetail/modules/shared/httphelpers"
)

func TestAuthenticationMiddleware(t *testing.T) {
	tests := []struct {
		name                string
		mode                config.AuthMode
		hasSessionToken     bool
		hasBearerToken      bool
		wantContextHasToken bool
	}{
		{"auto-mode without tokens", config.AuthModeAuto, false, false, false},
		{"auto-mode with session token", config.AuthModeAuto, true, false, false},
		{"auto-mode with bearer token", config.AuthModeAuto, false, true, true},
		{"auto-mode with both tokens", config.AuthModeAuto, true, true, true},
		{"token-mode without tokens", config.AuthModeToken, false, false, false},
		{"token-mode with session token", config.AuthModeToken, true, false, true},
		{"token-mode with bearer token", config.AuthModeToken, false, true, true},
		{"token-mode with both tokens", config.AuthModeToken, false, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// set up router
			router := gin.New()

			// add sessions middleware
			router.Use(sessions.Sessions("session", cookie.NewStore([]byte("xx"))))

			// custom middleware to add token to session
			router.Use(func(c *gin.Context) {
				if tt.hasSessionToken {
					session := sessions.Default(c)
					session.Set(k8sTokenSessionKey, "xxx-session")
					session.Save()
				}
			})

			// add auth middleware
			router.Use(authenticationMiddleware(tt.mode))

			// custom middleware to check result
			router.Use(func(c *gin.Context) {
				token, exists := c.Get(k8sTokenSessionKey)
				if tt.wantContextHasToken {
					assert.Equal(t, true, exists)

					// ensure that bearer token takes precedence
					if tt.hasBearerToken {
						assert.Equal(t, token, "xxx-bearer")
					} else {
						assert.Equal(t, token, "xxx-session")
					}
				} else {
					assert.Equal(t, false, exists)
				}
				c.Next()
			})

			// add route for testing
			router.GET("/", func(c *gin.Context) {
				c.String(http.StatusOK, "ok")
			})

			// execute request
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/", nil)
			if tt.hasBearerToken {
				r.Header.Add("Authorization", "Bearer xxx-bearer")
			}
			router.ServeHTTP(w, r)

			// check result
			assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		})
	}
}

// A whitespace-only bearer header must not pass the AuthModeToken gate.
// k8sAuthenticationMiddleware only checks `token == ""`, so the upstream
// authenticationMiddleware has to normalize before storing in the gin context.
func TestAuthenticationMiddlewareRejectsWhitespaceToken(t *testing.T) {
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("xx"))))
	router.Use(authenticationMiddleware(config.AuthModeToken))
	router.Use(k8sAuthenticationMiddleware(config.AuthModeToken))
	router.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer    ")
	router.ServeHTTP(w, r)

	assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
}

const csrfPreseededToken = "testtokenvalue1234"

// runCSRFCase executes one CSRF middleware test case and returns the response.
func runCSRFCase(t *testing.T, method string, header http.Header, seedToken bool) *http.Response {
	t.Helper()

	store := cookie.NewStore([]byte("secret"))
	store.Options(sessions.Options{Path: "/", Secure: false})

	router := gin.New()
	router.Use(sessions.Sessions("session", store))
	if seedToken {
		router.Use(func(c *gin.Context) {
			session := sessions.Default(c)
			session.Set(csrfTokenSessionKey, csrfPreseededToken)
			session.Save()
			c.Next()
		})
	}
	router.Use(csrfProtectionMiddleware())
	router.Any("/", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(method, "/", nil)
	for k, v := range header {
		r.Header[k] = v
	}
	router.ServeHTTP(w, r)
	return w.Result()
}

func TestCSRFProtectionMiddlewareAllows(t *testing.T) {
	tests := []struct {
		name      string
		method    string
		setHeader http.Header
		seedToken bool
	}{
		// Safe methods bypass the check (no state change to protect).
		{
			name:      "GET without Sec-Fetch-Site",
			method:    "GET",
			setHeader: http.Header{},
			seedToken: false,
		},
		{
			name:      "GET cross-site",
			method:    "GET",
			setHeader: http.Header{"Sec-Fetch-Site": []string{"cross-site"}},
			seedToken: false,
		},
		{
			name:      "HEAD without Sec-Fetch-Site",
			method:    "HEAD",
			setHeader: http.Header{},
			seedToken: false,
		},
		{
			name:      "OPTIONS without Sec-Fetch-Site",
			method:    "OPTIONS",
			setHeader: http.Header{},
			seedToken: false,
		},
		// Unsafe same-origin requests with a valid CSRF token are allowed.
		{
			name:      "POST same-origin with correct X-CSRF-Token",
			method:    "POST",
			setHeader: http.Header{"Sec-Fetch-Site": []string{"same-origin"}, "X-Csrf-Token": []string{csrfPreseededToken}},
			seedToken: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := runCSRFCase(t, tt.method, tt.setHeader, tt.seedToken)
			assert.Equal(t, http.StatusOK, resp.StatusCode)
		})
	}
}

func TestCSRFProtectionMiddlewareForbids(t *testing.T) {
	tests := []struct {
		name      string
		method    string
		setHeader http.Header
		seedToken bool
	}{
		// Unsafe methods require same-origin Sec-Fetch-Site.
		{
			name:      "POST without Sec-Fetch-Site",
			method:    "POST",
			setHeader: http.Header{},
			seedToken: false,
		},
		{
			name:      "POST cross-site",
			method:    "POST",
			setHeader: http.Header{"Sec-Fetch-Site": []string{"cross-site"}},
			seedToken: false,
		},
		{
			name:      "DELETE cross-site",
			method:    "DELETE",
			setHeader: http.Header{"Sec-Fetch-Site": []string{"cross-site"}},
			seedToken: false,
		},
		// Unsafe same-origin requests still require a valid CSRF token.
		{
			name:      "POST same-origin without session token",
			method:    "POST",
			setHeader: http.Header{"Sec-Fetch-Site": []string{"same-origin"}},
			seedToken: false,
		},
		{
			name:      "POST same-origin with no X-CSRF-Token header",
			method:    "POST",
			setHeader: http.Header{"Sec-Fetch-Site": []string{"same-origin"}},
			seedToken: true,
		},
		{
			name:      "POST same-origin with wrong X-CSRF-Token",
			method:    "POST",
			setHeader: http.Header{"Sec-Fetch-Site": []string{"same-origin"}, "X-Csrf-Token": []string{"wrongtoken"}},
			seedToken: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := runCSRFCase(t, tt.method, tt.setHeader, tt.seedToken)
			assert.Equal(t, http.StatusForbidden, resp.StatusCode)
		})
	}
}

func TestK8sTokenRequiredMiddleware(t *testing.T) {
	tests := []struct {
		name           string
		setMode        config.AuthMode
		setHasToken    bool
		wantStatusCode int
	}{
		{"auto-mode with session token", config.AuthModeAuto, true, http.StatusOK},
		{"auto-mode without session token", config.AuthModeAuto, false, http.StatusOK},
		{"token-mode with session token", config.AuthModeToken, true, http.StatusOK},
		{"token-mode without session token", config.AuthModeToken, false, http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// set up router
			router := gin.New()

			// custom middleware to set value
			router.Use(func(c *gin.Context) {
				if tt.setHasToken {
					c.Set(k8sTokenGinKey, "xxx")
				}

				c.Next()
			})

			// add middleware
			router.Use(k8sAuthenticationMiddleware(tt.setMode))

			// add route for testing
			router.GET("/", func(c *gin.Context) {
				c.String(http.StatusOK, "ok")
			})

			// execute request
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/", nil)
			router.ServeHTTP(w, r)

			// check result
			resp := w.Result()
			assert.Equal(t, "no-store", resp.Header["Cache-Control"][0])
			assert.Equal(t, tt.wantStatusCode, resp.StatusCode)
		})
	}
}

func TestWebSocketCSRFContextMiddleware(t *testing.T) {
	const sessTok = "session-csrf-token"

	tests := []struct {
		name          string
		hasSession    bool
		isUpgrade     bool
		clientHeader  string // value sent by client; "" = absent
		wantCtxValue  any
		wantOutHeader string
	}{
		{
			name:          "WS upgrade with session: ctx and header set from session",
			hasSession:    true,
			isUpgrade:     true,
			wantCtxValue:  sessTok,
			wantOutHeader: sessTok,
		},
		{
			name:         "non-WS request leaves ctx and header unset even with session",
			hasSession:   true,
			isUpgrade:    false,
			wantCtxValue: nil,
		},
		{
			name:         "WS upgrade without session leaves ctx and header unset",
			isUpgrade:    true,
			wantCtxValue: nil,
		},
		{
			name:         "client-supplied X-Forwarded-CSRF-Token is stripped (no session)",
			isUpgrade:    true,
			clientHeader: "spoofed",
			wantCtxValue: nil,
		},
		{
			name:          "client-supplied X-Forwarded-CSRF-Token is overwritten (with session)",
			hasSession:    true,
			isUpgrade:     true,
			clientHeader:  "spoofed",
			wantCtxValue:  sessTok,
			wantOutHeader: sessTok,
		},
		{
			name:         "non-WS request strips client-supplied X-Forwarded-CSRF-Token",
			isUpgrade:    false,
			clientHeader: "spoofed",
			wantCtxValue: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(sessions.Sessions("session", cookie.NewStore([]byte("xx"))))
			router.Use(func(c *gin.Context) {
				if tt.hasSession {
					session := sessions.Default(c)
					session.Set(csrfTokenSessionKey, sessTok)
					session.Save()
				}
				c.Next()
			})
			router.Use(websocketCSRFContextMiddleware())

			var (
				gotCtxValue  any
				gotOutHeader string
			)
			router.GET("/", func(c *gin.Context) {
				gotCtxValue = c.Request.Context().Value(graph.SessionCSRFTokenCtxKey)
				gotOutHeader = c.Request.Header.Get(httphelpers.HeaderForwardedCSRFToken)
				c.String(http.StatusOK, "ok")
			})

			r := httptest.NewRequest("GET", "/", nil)
			if tt.isUpgrade {
				r.Header.Set("Connection", "Upgrade")
				r.Header.Set("Upgrade", "websocket")
			}
			if tt.clientHeader != "" {
				r.Header.Set(httphelpers.HeaderForwardedCSRFToken, tt.clientHeader)
			}

			router.ServeHTTP(httptest.NewRecorder(), r)

			assert.Equal(t, tt.wantCtxValue, gotCtxValue)
			assert.Equal(t, tt.wantOutHeader, gotOutHeader)
		})
	}
}
