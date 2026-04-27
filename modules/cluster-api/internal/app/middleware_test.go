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

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/kubetail-org/kubetail/modules/cluster-api/graph"
	"github.com/kubetail-org/kubetail/modules/shared/grpchelpers"
	"github.com/kubetail-org/kubetail/modules/shared/httphelpers"
	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"
)

func TestAuthenticationMiddleware(t *testing.T) {
	tests := []struct {
		name       string
		setHeaders map[string]string
		wantToken  interface{}
	}{
		{
			"authorization header",
			map[string]string{
				"Authorization": "Bearer xxx",
			},
			"xxx",
		},
		{
			"x-forwarded-authorization header",
			map[string]string{
				"X-Forwarded-Authorization": "Bearer xxx",
			},
			"xxx",
		},
		{
			"prefers x-forwarded-authorization header",
			map[string]string{
				"Authorization":             "Bearer yyy",
				"X-Forwarded-Authorization": "Bearer zzz",
			},
			"zzz",
		},
		{
			"empty token",
			map[string]string{
				"Authorization": "",
			},
			nil,
		},
		{
			"malformed token",
			map[string]string{
				"Authorization": "xxx",
			},
			nil,
		},
		{
			"whitespace-only bearer is treated as absent",
			map[string]string{
				"Authorization": "Bearer    ",
			},
			nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Init router
			router := gin.New()

			// Add middleware
			router.Use(authenticationMiddleware)

			// Add route for testing
			router.GET("/", func(c *gin.Context) {
				// Check token
				ctx := c.Request.Context()

				// Check token for kubernetes requests
				val1 := ctx.Value(k8shelpers.K8STokenCtxKey)
				assert.Equal(t, tt.wantToken, val1)

				// Check token for gRPC requests
				val2 := ctx.Value(grpchelpers.K8STokenCtxKey)
				assert.Equal(t, tt.wantToken, val2)

				c.String(http.StatusOK, "ok")
			})

			// Build request
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/", nil)

			for key, val := range tt.setHeaders {
				r.Header.Add(key, val)
			}

			// Execute request
			router.ServeHTTP(w, r)

			// Check response
			assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		})
	}
}

func TestForwardedCSRFTokenMiddleware(t *testing.T) {
	tests := []struct {
		name      string
		setHeader string
		isUpgrade bool
		wantValue any
	}{
		{"upgrade with header", "abc123", true, "abc123"},
		{"upgrade without header", "", true, nil},
		{"non-upgrade ignores header", "abc123", false, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(forwardedCSRFTokenMiddleware)
			router.GET("/", func(c *gin.Context) {
				val := c.Request.Context().Value(graph.SessionCSRFTokenCtxKey)
				assert.Equal(t, tt.wantValue, val)
				c.String(http.StatusOK, "ok")
			})

			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/", nil)
			if tt.isUpgrade {
				r.Header.Set("Connection", "Upgrade")
				r.Header.Set("Upgrade", "websocket")
			}
			if tt.setHeader != "" {
				r.Header.Set(httphelpers.HeaderForwardedCSRFToken, tt.setHeader)
			}
			router.ServeHTTP(w, r)
			assert.Equal(t, http.StatusOK, w.Result().StatusCode)
		})
	}
}
