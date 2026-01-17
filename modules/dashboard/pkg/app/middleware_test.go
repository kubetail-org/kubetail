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

	"github.com/kubetail-org/kubetail/modules/dashboard/pkg/config"
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
