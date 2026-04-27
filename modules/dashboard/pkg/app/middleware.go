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
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"slices"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"

	"github.com/kubetail-org/kubetail/modules/dashboard/graph"
	"github.com/kubetail-org/kubetail/modules/dashboard/pkg/config"
	"github.com/kubetail-org/kubetail/modules/shared/ginhelpers"
	"github.com/kubetail-org/kubetail/modules/shared/httphelpers"
	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"
)

// allowedSecFetchSite defines the secure values for the Sec-Fetch-Site header.
var allowedSecFetchSite = []string{"same-origin"}

// safeMethods are HTTP methods that don't change state, so they don't need
// CSRF protection. Cross-origin reads are blocked by the Same-Origin Policy,
// and skipping them lets WebSocket upgrades through (Chrome does not send
// Sec-Fetch-Site on upgrade requests, which the WebSocket same-origin gate
// handles instead).
var safeMethods = []string{http.MethodGet, http.MethodHead, http.MethodOptions}

// getOrCreateCSRFToken returns the session's CSRF token, generating and
// persisting a new one if none exists yet.
func getOrCreateCSRFToken(session sessions.Session) (token string, isNew bool) {
	if val, ok := session.Get(csrfTokenSessionKey).(string); ok && val != "" {
		return val, false
	}

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	token = hex.EncodeToString(b)
	session.Set(csrfTokenSessionKey, token)
	return token, true
}

func csrfProtectionMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if slices.Contains(safeMethods, c.Request.Method) {
			c.Next()
			return
		}

		// Layer 1: Sec-Fetch-Site check
		if !slices.Contains(allowedSecFetchSite, c.GetHeader("Sec-Fetch-Site")) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}

		// Layer 2: CSRF token check
		session := sessions.Default(c)
		token, _ := session.Get(csrfTokenSessionKey).(string)
		if token == "" || c.GetHeader("X-CSRF-Token") != token {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}

		c.Next()
	}
}

// websocketCSRFContextMiddleware places the session's CSRF token into the
// request context (for the dashboard's WebSocket InitFunc) and stamps it as
// X-Forwarded-CSRF-Token (for the cluster-api proxy to forward upstream).
// Always strips any client-supplied X-Forwarded-CSRF-Token to prevent
// header smuggling.
func websocketCSRFContextMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Header.Del(httphelpers.HeaderForwardedCSRFToken)

		if !ginhelpers.IsWebSocketRequest(c) {
			c.Next()
			return
		}
		session := sessions.Default(c)
		if tok, ok := session.Get(csrfTokenSessionKey).(string); ok && tok != "" {
			ctx := context.WithValue(c.Request.Context(), graph.SessionCSRFTokenCtxKey, tok)
			c.Request = c.Request.WithContext(ctx)
			c.Request.Header.Set(httphelpers.HeaderForwardedCSRFToken, tok)
		}
		c.Next()
	}
}

func authenticationMiddleware(mode config.AuthMode) gin.HandlerFunc {
	return func(c *gin.Context) {
		var token string

		// Use cookie session in "token" mode
		if mode == config.AuthModeToken {
			session := sessions.Default(c)
			if val, ok := session.Get(k8sTokenSessionKey).(string); ok {
				token = val
			}
		}

		// check Authorization header
		header := c.GetHeader("Authorization")
		if after, ok := strings.CutPrefix(header, "Bearer "); ok {
			token = after
		}

		// Trim before deciding presence — without this, a whitespace-only
		// bearer header (e.g. "Authorization: Bearer    ") would bypass the
		// AuthModeToken gate downstream, since k8sAuthenticationMiddleware
		// only checks `token == ""`.
		token = strings.TrimSpace(token)

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
