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
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/kubetail-org/kubetail/modules/cluster-api/graph"
	"github.com/kubetail-org/kubetail/modules/shared/ginhelpers"
	"github.com/kubetail-org/kubetail/modules/shared/grpchelpers"
	"github.com/kubetail-org/kubetail/modules/shared/httphelpers"
	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"
)

// Add user to context if authenticated
func authenticationMiddleware(c *gin.Context) {
	var token string

	// Check X-Forwarded-Authorization & Authorization headers
	header := c.GetHeader("X-Forwarded-Authorization")
	if header == "" {
		header = c.GetHeader("Authorization")
	}
	if strings.HasPrefix(header, "Bearer ") {
		token = strings.TrimPrefix(header, "Bearer ")
	}

	// Trim at the trust boundary so consumers can rely on a simple `token == ""`
	// presence check; otherwise a whitespace-only bearer header would slip past
	// any gate that doesn't itself trim.
	token = strings.TrimSpace(token)

	// Add to context for kubernetes requests
	if token != "" {
		ctx := context.WithValue(c.Request.Context(), k8shelpers.K8STokenCtxKey, token)
		c.Request = c.Request.WithContext(ctx)
	}

	// Add to context for gRPC requests
	if token != "" {
		ctx := context.WithValue(c.Request.Context(), grpchelpers.K8STokenCtxKey, token)
		c.Request = c.Request.WithContext(ctx)
	}

	// Continue
	c.Next()
}

// forwardedCSRFTokenMiddleware copies X-Forwarded-CSRF-Token (set by the
// dashboard reverse proxy when forwarding browser upgrades) into the request
// context so the GraphQL WebSocket InitFunc can validate connection_init.
func forwardedCSRFTokenMiddleware(c *gin.Context) {
	if !ginhelpers.IsWebSocketRequest(c) {
		c.Next()
		return
	}
	if tok := c.GetHeader(httphelpers.HeaderForwardedCSRFToken); tok != "" {
		ctx := context.WithValue(c.Request.Context(), graph.SessionCSRFTokenCtxKey, tok)
		c.Request = c.Request.WithContext(ctx)
	}
	c.Next()
}

// requireTokenMiddleware aborts with 401 when no bearer token reached the
// request context. Pair with authenticationMiddleware (which extracts the
// token from headers) so all dynamic routes share a single auth check.
func requireTokenMiddleware(c *gin.Context) {
	token, _ := c.Request.Context().Value(k8shelpers.K8STokenCtxKey).(string)
	if strings.TrimSpace(token) == "" {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	c.Next()
}
