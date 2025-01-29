// Copyright 2024-2025 Andres Morey
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

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"

	"github.com/kubetail-org/kubetail/modules/shared/config"
	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"
)

func authenticationMiddleware(mode config.AuthMode) gin.HandlerFunc {
	return func(c *gin.Context) {
		// continue if not in token mode
		if mode != config.AuthModeToken {
			c.Next()
			return
		}

		var token string

		// check cookie session
		session := sessions.Default(c)
		if val, ok := session.Get(k8sTokenSessionKey).(string); ok {
			token = val
		}

		// check Authorization header
		header := c.GetHeader("Authorization")
		if strings.HasPrefix(header, "Bearer ") {
			token = strings.TrimPrefix(header, "Bearer ")
		}

		// if present, add token to gin context
		if token != "" {
			// Add to gin context
			c.Set(k8sTokenGinKey, token)
		}

		// continue with the request
		c.Next()
	}
}

func k8sAuthenticationMiddleware(mode config.AuthMode) gin.HandlerFunc {
	return func(c *gin.Context) {
		// set "Cache-Control: no-store" so that pages aren't stored in the users browser cache
		c.Header("Cache-Control", "no-store")

		// Get token from gin session
		token := c.GetString(k8sTokenGinKey)

		// Reject unauthenticated requests if auth-mode: token
		if mode == config.AuthModeToken && token == "" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		// If token is present add to request context for Kubernetes requests downstream
		if token != "" {
			// Add to request context for kubernetes requests downstream
			ctx := context.WithValue(c.Request.Context(), k8shelpers.K8STokenCtxKey, token)

			c.Request = c.Request.WithContext(ctx)
		}

		c.Next()
	}
}
