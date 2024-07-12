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
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestAuthenticationMiddleware(t *testing.T) {
	tests := []struct {
		name                string
		mode                AuthMode
		hasSessionToken     bool
		hasBearerToken      bool
		wantContextHasToken bool
	}{
		{"cluster-mode without tokens", AuthModeCluster, false, false, false},
		{"cluster-mode with session token", AuthModeCluster, true, false, false},
		{"cluster-mode with bearer token", AuthModeCluster, false, true, false},
		{"cluster-mode with both tokens", AuthModeCluster, true, true, false},
		{"local-mode without tokens", AuthModeLocal, false, false, false},
		{"local-mode with session token", AuthModeLocal, true, false, false},
		{"local-mode with bearer token", AuthModeLocal, false, true, false},
		{"local-mode with both tokens", AuthModeLocal, true, true, false},
		{"token-mode without tokens", AuthModeToken, false, false, false},
		{"token-mode with session token", AuthModeToken, true, false, true},
		{"token-mode with bearer token", AuthModeToken, false, true, true},
		{"token-mode with both tokens", AuthModeToken, false, true, true},
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
				token, exists := c.Get(k8sTokenCtxKey)
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
		name     string
		hasToken bool
	}{
		{"Request with token", true},
		{"Request without token", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// set up router
			router := gin.New()

			// custom middleware to set value
			router.Use(func(c *gin.Context) {
				if tt.hasToken {
					c.Set(k8sTokenCtxKey, "xxx")
				}
				c.Next()
			})

			// add middleware
			router.Use(k8sTokenRequiredMiddleware)

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
			if tt.hasToken {
				assert.Equal(t, http.StatusOK, resp.StatusCode)
			} else {
				assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
			}
		})
	}
}
